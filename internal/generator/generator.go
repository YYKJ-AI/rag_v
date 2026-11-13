// Package generator 可选的云端表述改写网关（P2.0 最小实现）。
// 纪律：模型只润色表述；citations、裁决、拒答全部由确定性引擎产出。
// 任何失败（配置缺失/网络/超时/空输出）都返回 error，由调用方回退模板路径。
package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ragbot/internal/safety"
)

// ErrStatusChanged 模型改写把"可回答"改成了"无法回答"——语义判定层的漏判信号
// （判定层放行的证据被生成模型认为不支持所问事项）。调用方据此转为拒答并记录
// judge_miss，不得静默回退模板掩盖。
var ErrStatusChanged = errors.New("模型表述改变了回答状态")

// Config Generator 配置（从 .env / 环境变量加载）
type Config struct {
	Enabled  bool
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
	Timeout  time.Duration
}

// LoadConfigFile 读取 .env 形式的 KEY=VALUE 文件（忽略空行/注释）。
// 不覆盖已存在的进程环境变量。
func LoadConfigFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists && v != "" {
			env[k] = v
		}
	}
	return env, nil
}

// LoadConfig 从 env 映射构建 Generator 配置。
// 只有 ENABLED=true 且 BaseURL/Model 非空才算有效启用。
func LoadConfig(env map[string]string) Config {
	get := func(k string) string { return strings.TrimSpace(env[k]) }
	cfg := Config{
		Enabled:  strings.EqualFold(get("RAGBOT_GENERATOR_ENABLED"), "true"),
		Provider: get("RAGBOT_GENERATOR_PROVIDER"),
		BaseURL:  strings.TrimRight(get("RAGBOT_GENERATOR_BASE_URL"), "/"),
		Model:    get("RAGBOT_GENERATOR_MODEL"),
		APIKey:   get("RAGBOT_GENERATOR_API_KEY"),
		Timeout:  30 * time.Second,
	}
	if v := get("RAGBOT_GENERATOR_TIMEOUT_SECONDS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 && n <= 300 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai_compatible"
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		cfg.Enabled = false // 缺关键配置即视为未启用
	}
	return cfg
}

// LoadConfigFromPath 便捷入口：文件不存在 → 返回禁用配置（不是错误）。
func LoadConfigFromPath(path string) Config {
	env, err := LoadConfigFile(path)
	if err != nil {
		return Config{}
	}
	return LoadConfig(env)
}

// Valid 是否可以实际发起请求
func (c Config) Valid() bool { return c.Enabled && c.BaseURL != "" && c.Model != "" }

// Gateway OpenAI 兼容流式客户端（含运行时启停开关：用于测试模型断线的真回退）
type Gateway struct {
	cfg    Config
	client *http.Client
	mu     sync.RWMutex
	on     bool // 运行时开关；初始值取 cfg.Enabled，可被 SetEnabled 切换
}

func NewGateway(cfg Config) *Gateway {
	return &Gateway{cfg: cfg, on: cfg.Enabled, client: &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Enabled 运行时是否可用（开关 + 基础配置完备）。
func (g *Gateway) Enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.on && g.cfg.BaseURL != "" && g.cfg.Model != ""
}

// SetEnabled 运行时切换模型路线（false=离线模式，走确定性链路）。
func (g *Gateway) SetEnabled(v bool) {
	g.mu.Lock()
	g.on = v
	g.mu.Unlock()
}

// Info 当前模型配置摘要（密钥不出网关）。
func (g *Gateway) Info() (provider, model string, configured bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.cfg.Provider, g.cfg.Model, g.cfg.BaseURL != "" && g.cfg.Model != ""
}

const systemGuard = "你是制度知识库的回答改写助手。规则：" +
	"1) 只能基于用户提供的\"既有回答\"和\"证据摘录\"改写表述，不得新增任何事实、数字、日期或来源；" +
	"2) 不得删除既有回答中的关键数字、版本号和条件；" +
	"3) 不得添加前言客套或元评论；直接输出改写后的回答正文；" +
	"4) 如果既有回答本身表示证据不足或冲突，原样保留该结论，不做修饰。"

// StreamAnswer 用云端模型流式改写确定性回答。
// The upstream may stream, but onDelta receives only the completed response.
// A truncated or malformed stream must never reach the client as a partial answer.
func (g *Gateway) StreamAnswer(ctx context.Context, query string, baseAnswer string, evidence []string, onDelta func(string)) (string, error) {
	return g.streamAnswer(ctx, query, baseAnswer, evidence, "", onDelta)
}

// StreamAnswerWithHint 带核验反馈的重试入口：hint 会附加到用户消息末尾，
// 用于把"上次改写为何被拒"反馈给模型（回退能回答的题，模型也应该能回答）。
func (g *Gateway) StreamAnswerWithHint(ctx context.Context, query string, baseAnswer string, evidence []string, hint string, onDelta func(string)) (string, error) {
	return g.streamAnswer(ctx, query, baseAnswer, evidence, hint, onDelta)
}

func (g *Gateway) streamAnswer(ctx context.Context, query string, baseAnswer string, evidence []string, hint string, onDelta func(string)) (string, error) {
	if !g.Enabled() {
		return "", fmt.Errorf("generator 未启用或已被离线开关关闭")
	}
	var ev strings.Builder
	for i, e := range evidence {
		if strings.TrimSpace(e) == "" {
			continue
		}
		fmt.Fprintf(&ev, "[%d] %s\n", i+1, e)
	}
	userContent := fmt.Sprintf("问题：%s\n\n既有回答（改写对象，事实以此为准）：\n%s\n\n证据摘录：\n%s", query, baseAnswer, ev.String())
	if strings.TrimSpace(hint) != "" {
		userContent += "\n\n核验反馈（必须遵守）：" + hint
	}

	payload := map[string]any{
		"model":  g.cfg.Model,
		"stream": true,
		"messages": []map[string]string{
			{"role": "system", "content": systemGuard},
			{"role": "user", "content": userContent},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := g.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("generator 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("generator 返回状态 %d", resp.StatusCode)
	}

	var full strings.Builder
	done := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("generator 流响应格式错误")
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			full.WriteString(chunk.Choices[0].Delta.Content)
			if full.Len() > 16<<10 {
				return "", fmt.Errorf("generator 返回内容过长")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("generator 流读取失败: %w", err)
	}
	if !done {
		return "", fmt.Errorf("generator 流未正常结束")
	}
	out := strings.TrimSpace(full.String())
	if out == "" {
		return "", fmt.Errorf("generator 返回空内容")
	}
	if onDelta != nil {
		onDelta(out)
	}
	return out, nil
}

// ---- 查询预处理层（在线）：改写 / 实体抽取 / 子问题拆分 / 时间提取 ----
// 纪律：预处理只产出"检索计划"，供确定性管线消费；不裁决、不生成答案。
// 防实体漂移：禁止引入原问题未提及的实体；子查询含指令样式文本时直接丢弃。

// QueryPlan 查询预处理的结构化产出。
type QueryPlan struct {
	Rewritten  string   `json:"rewritten"`
	Entities   []string `json:"entities"`
	SubQueries []string `json:"sub_queries"`
	Time       string   `json:"time"`           // YYYY-MM-DD 或空
	TimePrec   string   `json:"time_precision"` // day / month / year（年份=全年、月份=全月口径）
}

const preprocessSystem = "你是知识库检索的查询预处理助手。任务：对用户问题做轻度改写、实体抽取和子问题拆分。" +
	"规则：1) 严格基于用户问题本身，禁止引入问题未提及的实体、活动名、机构、时间或主题；" +
	"2) entities 抽取问题中出现的专名（活动/机构/场所/对象），没有则输出空数组；" +
	"3) sub_queries 把复合问题拆成最多 4 个各自可独立检索的子问题；单一简单问题输出一条与原问题同义的即可；" +
	"4) time 仅当问题明确包含日期/年份时输出 YYYY-MM-DD，并同时给出 time_precision：" +
		"完整日期→day（time 如 2026-03-15）；某年某月→month（time 如 2025-08-01）；仅年份→year（time 如 2025-01-01，表示该年全年口径）；两者都无则 time 留空；" +
	"5) rewritten 是去口语化的同义改写，不得改变含义，拿不准就与原问题相同；" +
	"6) 只输出 JSON：{\"rewritten\":\"...\",\"entities\":[],\"sub_queries\":[],\"time\":\"\"}，不要输出任何解释。"

// PreprocessQuery 在线预处理：模型产出结构化检索计划。
// 任何失败（未启用/网络/解析/空产出）都返回 error，由调用方回退原查询直检。
func (g *Gateway) PreprocessQuery(ctx context.Context, query string) (QueryPlan, error) {
	if !g.Enabled() {
		return QueryPlan{}, errors.New("generator 未启用或已被离线开关关闭")
	}
	payload := map[string]any{
		"model":  g.cfg.Model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": preprocessSystem},
			{"role": "user", "content": "用户问题：" + query},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return QueryPlan{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return QueryPlan{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return QueryPlan{}, fmt.Errorf("预处理请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return QueryPlan{}, fmt.Errorf("预处理返回状态 %d", resp.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return QueryPlan{}, fmt.Errorf("预处理响应格式错误")
	}
	if len(completion.Choices) == 0 {
		return QueryPlan{}, errors.New("预处理无产出")
	}
	return parseQueryPlan(completion.Choices[0].Message.Content)
}

func parseQueryPlan(content string) (QueryPlan, error) {
	text := strings.TrimSpace(content)
	// 容错：剥掉 markdown 代码栅栏
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var plan QueryPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		return QueryPlan{}, fmt.Errorf("预处理 JSON 解析失败")
	}
	// 实体：清洗、限量
	plan.Entities = cleanList(plan.Entities, 8, 30)
	// 子查询：清洗、限量、防注入（指令样式文本直接丢弃）
	subs := cleanList(plan.SubQueries, 6, 60)
	kept := make([]string, 0, len(subs))
	for _, s := range subs {
		if safety.IsDirectOverride(s) {
			continue
		}
		kept = append(kept, s)
		if len(kept) == 4 {
			break
		}
	}
	plan.SubQueries = kept
	plan.Rewritten = strings.TrimSpace(plan.Rewritten)
	if len([]rune(plan.Rewritten)) > 120 || safety.IsDirectOverride(plan.Rewritten) {
		plan.Rewritten = ""
	}
	// 时间：合法日期才放行，否则置空交正则兜底；精度仅接受 day/month/year
	if plan.Time != "" {
		if _, err := time.Parse("2006-01-02", plan.Time); err != nil {
			plan.Time = ""
			plan.TimePrec = ""
		}
	}
	switch plan.TimePrec {
	case "day", "month", "year":
	default:
		plan.TimePrec = ""
	}
	if plan.Time == "" {
		plan.TimePrec = ""
	}
	if len(plan.SubQueries) == 0 {
		return plan, errors.New("预处理未产出可用子问题")
	}
	return plan, nil
}

func cleanList(items []string, maxCount, maxRunes int) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" || len([]rune(it)) > maxRunes {
			continue
		}
		dup := false
		for _, o := range out {
			if o == it {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, it)
		}
		if len(out) == maxCount {
			break
		}
	}
	return out
}

// AnchorTime 按时间精度换算业务日期锚点——与引擎正则推断的"期末含全期"约定一致：
// year → 当年 12 月 31 日（该年任意时点有效的版本都参与裁决）；
// month → 当月月末；day → 当日。空精度按 day 处理。
// 这保证"2025年申请"不会被钉死在 1 月 1 日（彼时 v1 尚未生效）而误拒。
func AnchorTime(t time.Time, precision string) time.Time {
	switch precision {
	case "year":
		return time.Date(t.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
	default:
		return t
	}
}
