// Package generator 可选的云端表述改写网关（P2.0 最小实现）。
// 纪律：模型只润色表述；citations、裁决、拒答全部由确定性引擎产出。
// 任何失败（配置缺失/网络/超时/空输出）都返回 error，由调用方回退模板路径。
package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

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

// Gateway OpenAI 兼容流式客户端
type Gateway struct {
	cfg    Config
	client *http.Client
}

func NewGateway(cfg Config) *Gateway {
	return &Gateway{cfg: cfg, client: &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
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
	if !g.cfg.Valid() {
		return "", fmt.Errorf("generator 未启用或配置不完整")
	}
	var ev strings.Builder
	for i, e := range evidence {
		if strings.TrimSpace(e) == "" {
			continue
		}
		fmt.Fprintf(&ev, "[%d] %s\n", i+1, e)
	}
	userContent := fmt.Sprintf("问题：%s\n\n既有回答（改写对象，事实以此为准）：\n%s\n\n证据摘录：\n%s",
		query, baseAnswer, ev.String())

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
