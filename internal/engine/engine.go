// Package engine 确定性回答编排：证据抽取 → 冲突三段式 → 模板组装/拒答。
// 无模型依赖；LLM 只是可插拔的表述增强（generator 包），失败自动回退本路径。
package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ragbot/internal/tokenize"
	"ragbot/internal/types"
)

// Engine 回答编排器
type Engine struct {
	R                retrieverIface
	lastInferredTime bool // 最近一次 Answer 是否从问句推断出时间点
}

// InferredTimeTime 供 API 层报告"本次回答是否基于问句推断的时间点"
func (e *Engine) InferredTime() bool { return e.lastInferredTime }

type retrieverIface interface {
	SearchVerified(query string, n int) []types.Hit
	VerifiedDocs() []*types.Document
	Section(id string) (types.Section, bool)
}

func New(r retrieverIface) *Engine { return &Engine{R: r} }

// 拒答判据（全确定性；阈值作用在检索分数与对齐分上，不碰模型输出）
const (
	absFloor   = 0.15 // 绝对下限（故意很松，防误杀）
	alignFloor = 0.0  // 无任何句子对齐即视为没答案
)

// Answer 回答入口。asOf 参数为显式覆盖；nil 时自动从问句文本推断时间点
// （"2025年…"→历史口径，无时间表达→现行口径）。多版本演变自动呈现时间线。
func (e *Engine) Answer(query string, asOf *time.Time) types.Answer {
	return e.AnswerWithScope(query, asOf, "")
}

// AnswerWithScope answers using only documents whose scope_keys include scopeKey or *.
// An empty scopeKey keeps the existing unscoped behavior.
func (e *Engine) AnswerWithScope(query string, asOf *time.Time, scopeKey string) types.Answer {
	inferred := false
	if asOf == nil {
		asOf = inferQueryTime(query)
		inferred = asOf != nil
	}
	e.lastInferredTime = inferred
	if asOf == nil {
		localNow := time.Now()
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
		asOf = &today
	}
	hits := e.filterByAsOf(e.R.SearchVerified(query, 0), asOf, scopeKey)
	if len(hits) > 8 {
		hits = hits[:8]
	}
	if e.weak(query, hits) {
		return types.Answer{
			Status: "insufficient_evidence",
			Answer: "资料中未找到覆盖该问题的充分依据，无法给出有出处的回答。",
		}
	}
	evids := e.extract(query, hits)
	if len(splitQueryClauses(query)) == 1 {
		if answer, handled := e.answerFromPointer(query, evids, *asOf, scopeKey); handled {
			return answer
		}
	}
	evids = withoutPointerDeclarations(evids)
	if clauses := splitQueryClauses(query); len(clauses) > 1 {
		var combined []evidence
		for _, clause := range clauses {
			focus := stripTitleTopic(clause, e.R.VerifiedDocs())
			candidates := withoutPointerDeclarations(e.extract(focus, hits))
			if len(candidates) == 0 || needsUnsupportedReasoning(clause, candidates) || (isModalQuestion(clause) && !hasModalVerdict(candidates)) {
				return types.Answer{Status: "insufficient_evidence", Answer: "至少一个子问题缺少可直接核验的证据，无法合并回答。"}
			}
			if groups := e.groupByClaim(clause, candidates); len(groups) > 1 {
				return e.buildConflict(groups)
			}
			combined = append(combined, candidates[0])
		}
		return e.build(combined)
	}

	if len(evids) == 0 {
		return types.Answer{
			Status: "insufficient_evidence",
			Answer: "检索到相关资料，但其中没有与问题直接对应的结论性内容，无法给出有出处的回答。",
		}
	}
	if needsUnsupportedReasoning(query, evids) {
		return types.Answer{
			Status: "insufficient_evidence",
			Answer: "资料中没有直接给出所问的计算或推断结论，当前离线规则不能据此生成可核验的答案。",
		}
	}

	// 是非问句（"可以…吗/能不能…"）需要明确的许可/禁止性表述；
	// 证据里没有这类语义 → 不允许用无关相似内容拼出貌似成功的回答。
	if isModalQuestion(query) && !hasModalVerdict(evids) {
		return types.Answer{
			Status: "insufficient_evidence",
			Answer: "资料中未找到对该问题的明确许可或禁止性规定，无法给出有出处的回答。",
		}
	}

	// 冲突三段式：同题多来源且值不同（只在各组的"最强证据句"之间判定，防弱相关污染）
	if groups := e.groupByClaim(query, evids); len(groups) > 1 {
		// 生效日和明确替代关系已在证据筛选时裁决；剩余互斥证据交人工核实。
		return e.buildConflict(groups)
	}
	return e.build(evids)
}

// sharedTitleTopic finds a topic repeated across several document titles.
func sharedTitleTopic(clause string, docs []*types.Document) string {
	best := ""
	q := []rune(clause)
	for _, doc := range docs {
		title := doc.Title
		for start := range q {
			for end := start + 3; end <= len(q); end++ {
				candidate := string(q[start:end])
				if len([]rune(best)) >= end-start || !strings.Contains(title, candidate) {
					continue
				}
				count := 0
				for _, other := range docs {
					if strings.Contains(other.Title, candidate) {
						count++
					}
				}
				if count >= 3 {
					best = candidate
				}
			}
		}
	}
	return best
}

// stripTitleTopic removes that topic so answer terms drive sentence ranking.
func stripTitleTopic(clause string, docs []*types.Document) string {
	best := sharedTitleTopic(clause, docs)
	if best == "" {
		return clause
	}
	trimmed := strings.TrimSpace(strings.ReplaceAll(clause, best, ""))
	if trimmed == "" {
		return clause
	}
	return trimmed
}

func splitQueryClauses(query string) []string {
	var clauses []string
	for _, clause := range strings.FieldsFunc(query, func(r rune) bool { return r == '，' || r == '；' || r == ';' }) {
		if clause = strings.TrimSpace(clause); clause != "" {
			clauses = append(clauses, clause)
		}
	}
	return clauses
}

// extract 证据句抽取：命中父块内句子与 query 的对齐分过滤
type evidence struct {
	Sentence string
	Sec      types.Section
	Chunk    types.Chunk
	Title    string  // 所属文档标题（冲突主题门控用）
	Score    float64 // 上下文对齐分（排序与相对截断基准）
}

func (e *Engine) extract(query string, hits []types.Hit) []evidence {
	focusQuery := stripTitleTopic(query, e.R.VerifiedDocs())
	topic := sharedTitleTopic(query, e.R.VerifiedDocs())
	q := tokenize.Expand(tokenize.Tokenize(focusQuery))
	var out []evidence
	for _, h := range hits {
		title := docTitle(e, h.Section)
		for _, sent := range splitSentences(h.Section.Text) {
			if topic != "" && !strings.Contains(title, topic) && !strings.Contains(sent, topic) {
				continue
			}
			if strings.HasPrefix(sent, "|") && !sharesSpecificToken(q, tokenize.Tokenize(sent)) {
				continue // 表格行至少要命中问句中的具体单元格词
			}
			// 问答汇编：问：…答：… 同句。取"答："之后的答案部分做证据；纯问句跳过。
			if strings.HasPrefix(sent, "问：") {
				if i := strings.Index(sent, "答："); i >= 0 {
					sent = sent[i+len("答："):]
				} else {
					continue
				}
			}
			// 对齐上下文 = 文档标题 + 节路径 + 句子（标题/路径承载语义，如"申请条件"）
			ctx := title + " " + h.Section.SectionPath + " " + sent
			st := tokenize.Tokenize(ctx)
			score := alignScore(q, st)
			if isEligibilityQuery(focusQuery) && strings.Contains(sent, "申请人") {
				score += 0.40
			}
			if score >= 0.34 { // 句子级对齐阈值（dev 集调参）
				out = append(out, evidence{Sentence: sent, Sec: h.Section, Chunk: h.Chunk, Title: title, Score: score})
			}
		}
	}
	boostSpecificTerms(focusQuery, out)
	// 相对截断：只保留接近最强证据的句子（绝对阈值兜底），压掉长尾噪音引用
	if len(out) > 0 {
		best := 0.0
		for _, ev := range out {
			if ev.Score > best {
				best = ev.Score
			}
		}
		cut := 0.6 * best
		if cut < absFloor {
			cut = absFloor
		}
		filtered := out[:0]
		for _, ev := range out {
			if ev.Score >= cut {
				filtered = append(filtered, ev)
			}
		}
		out = filtered
	}
	// 按对齐质量稳定排序（分数相同按 SectionID，保证确定性）
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Sec.ID < out[j].Sec.ID
	})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func isEligibilityQuery(query string) bool {
	for _, marker := range []string{"资格", "条件", "谁可以", "什么人"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

// boostSpecificTerms rewards query terms that occur in few candidate sentences.
// It uses the actual evidence text, so a common document title cannot alone
// make an unrelated sentence outrank a specific answer.
func boostSpecificTerms(query string, evids []evidence) {
	terms := map[string]bool{}
	for _, term := range tokenize.Expand(tokenize.Tokenize(query)) {
		terms[term] = true
	}
	frequency := map[string]int{}
	for term := range terms {
		for _, ev := range evids {
			if strings.Contains(ev.Sentence, term) {
				frequency[term]++
			}
		}
	}
	for i := range evids {
		for term, count := range frequency {
			if count == 0 || !strings.Contains(evids[i].Sentence, term) {
				continue
			}
			weight := 0.08
			if len([]rune(term)) > 1 {
				weight = 0.30
			}
			evids[i].Score += weight / float64(count)
		}
	}
}

func sharesSpecificToken(query, sentence []string) bool {
	terms := make(map[string]bool, len(query))
	for _, term := range query {
		if len([]rune(term)) >= 2 {
			terms[term] = true
		}
	}
	for _, term := range sentence {
		if terms[term] {
			return true
		}
	}
	return false
}

// alignScore 对齐分：查询单字对句子的包含度（inter/|q_unigrams|）+ 条件词加权。
// 用单字而非 bigram：bigram 分母会稀释短句包含度，单字对中文查询-句子对齐最稳。
func alignScore(q, s []string) float64 {
	qu := unigrams(q)
	if len(qu) == 0 {
		return 0
	}
	sa := unigramSet(s)
	inter := 0
	for _, t := range qu {
		if sa[t] {
			inter++
		}
	}
	base := float64(inter) / float64(len(qu))
	if base == 0 {
		return 0
	}
	join := strings.Join(s, "")
	weight := 1.0
	for _, kw := range []string{"条件", "要求", "应当", "须", "必须", "不得"} {
		if strings.Contains(join, kw) {
			weight += 0.15
			break
		}
	}
	w := base * weight
	if w > 1 {
		w = 1
	}
	return w
}

func unigrams(toks []string) []string {
	var out []string
	for _, t := range toks {
		if len([]rune(t)) == 1 {
			out = append(out, t)
		}
	}
	return out
}

func unigramSet(toks []string) map[string]bool {
	m := map[string]bool{}
	for _, t := range toks {
		if len([]rune(t)) == 1 {
			m[t] = true
		}
	}
	return m
}

// groupByClaim 按"同一主题节下的不同数值结论"分组。
// 每组只保留最强对齐的一句；主题门控要求命中的标题字符是"文档家族特有"
// （低标题词频），防止"审批流程"这类查询仅凭"艺术节"通用字拖入无关冲突对。
func (e *Engine) groupByClaim(query string, evids []evidence) map[string][]evidence {
	q := tokenize.Expand(tokenize.Tokenize(query))
	best := map[string]evidence{}
	bestScore := map[string]float64{}
	for _, ev := range evids {
		key := ev.Sec.DocID + "@" + ev.Sec.Version
		if !e.titleSpecific(query, ev.Title) {
			continue // 主题不相关：命中的只是全库通用标题字
		}
		s := alignScore(q, tokenize.Tokenize(ev.Sentence))
		if s >= bestScore[key] {
			bestScore[key] = s
			best[key] = ev
		}
	}
	if len(best) <= 1 {
		return nil
	}
	// 检查组间最优句是否互斥
	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if mutuallyExclusive([]evidence{best[keys[i]]}, []evidence{best[keys[j]]}) {
				out := map[string][]evidence{}
				for _, k := range keys {
					out[k] = []evidence{best[k]}
				}
				return out
			}
		}
	}
	return nil
}

func mutuallyExclusive(a, b []evidence) bool {
	for _, ea := range a {
		for _, eb := range b {
			// 同一主题节 + 双方都有数值型结论 + 两句高度相似仅数值不同 → 数值型互斥
			if ea.Sec.SectionPath != eb.Sec.SectionPath {
				continue
			}
			if normalize(ea.Sentence) == normalize(eb.Sentence) {
				continue
			}
			if !hasNumericClaim(ea.Sentence) || !hasNumericClaim(eb.Sentence) {
				continue
			}
			// 两句互含度 >0.7（只差关键数值）才算互斥，避免"同节不同话题"误判
			if mutualContainment(ea.Sentence, eb.Sentence) > 0.7 {
				return true
			}
		}
	}
	return false
}

// mutualContainment 两句单字互含度（以较短句为基准）
func mutualContainment(a, b string) float64 {
	ua := unigrams(tokenize.Tokenize(a))
	ub := unigramSet(tokenize.Tokenize(b))
	inter := 0
	for _, t := range ua {
		if ub[t] {
			inter++
		}
	}
	base := len(ua)
	if len(ub) < base {
		base = len(ub)
	}
	if base == 0 {
		return 0
	}
	return float64(inter) / float64(base)
}

// normalize 去空白与标点，用于句子等值判断
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n':
		case r == '，' || r == '。' || r == '；' || r == ',' || r == '.' || r == ';':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hasNumericClaim 是否含数值型结论（数字/数词后跟量词，排除"一并"这类虚用）
func hasNumericClaim(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	measures := []string{"元", "个", "项", "次", "天", "年", "日", "倍", "份", "人", "周"}
	nums := []string{"一", "两", "三", "四", "五", "六", "七", "八", "九", "十", "百", "千", "万"}
	for _, n := range nums {
		if !strings.Contains(s, n) {
			continue
		}
		for _, m := range measures {
			if strings.Contains(s, n+m) {
				return true
			}
		}
	}
	return false
}

// businessActive 业务时间轴判定（as_of 历史查询）：
// 该文档在 asOf 时点是否有效 = 生效日期已到 且 未被已在 asOf 生效的新版本替代。
// 用 front-matter 业务日期（effective_date/replaces），不用系统导入时间。
func (e *Engine) businessActive(d *types.Document, asOf time.Time, scopeKey string) bool {
	if d.EffectiveDate.After(asOf) {
		return false // 该时点尚未生效
	}
	if !d.ValidUntil.IsZero() && !asOf.Before(d.ValidUntil.AddDate(0, 0, 1)) {
		return false
	}
	for _, other := range e.R.VerifiedDocs() {
		if other == d || other.EffectiveDate.Before(d.EffectiveDate) || other.EffectiveDate.After(asOf) || !matchesScope(other.ScopeKeys, scopeKey) {
			continue
		}
		if containsID(other.Replaces, d.ID+"@"+d.Version) {
			return false
		}
	}
	return true
}

func containsID(list []string, key string) bool {
	// 支持三种写法：完整 key（doc@v）、纯 doc_id、doc_id 前缀匹配
	base := key
	if i := strings.IndexByte(key, '@'); i >= 0 {
		base = key[:i]
	}
	for _, s := range list {
		if s == key || s == base {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]evidence) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// titleSpecific 主题门控：问句与该文档标题命中的字符里，是否存在"文档家族特有"
// 的字（标题词频 ≤2 份文档）——过滤"艺术节"这类全库通用字带来的假相关。
func (e *Engine) titleSpecific(query, title string) bool {
	qt := unigramSet(tokenize.Tokenize(query))
	for _, t := range tokenize.Tokenize(title) {
		if len([]rune(t)) != 1 || !qt[t] {
			continue
		}
		df := 0
		for _, d := range e.R.VerifiedDocs() {
			if strings.Contains(d.Title, t) {
				df++
			}
		}
		if df <= 2 {
			return true
		}
	}
	return false
}

// buildConflict 冲突标注：展示双方来源，交人工核实，绝不悄悄混合
func (e *Engine) buildConflict(groups map[string][]evidence) types.Answer {
	var b strings.Builder
	b.WriteString("检测到不同资料的表述可能相互矛盾，且资料未提供明确的生效依据，请人工核实：\n")
	var cits []types.Citation
	for _, k := range sortedKeys(groups) {
		evs := groups[k]
		if len(evs) == 0 {
			continue
		}
		s := evs[0].Sec
		b.WriteString(fmt.Sprintf("- 《%s》%s（%s）："+"%s\n", docTitle(e, s), s.Version, s.SectionPath, evs[0].Sentence))
		cits = append(cits, types.Citation{
			DocumentID: s.DocID, Version: s.Version, Location: s.SectionPath, Excerpt: evs[0].Sentence,
		})
	}
	return types.Answer{Status: "conflict", Answer: b.String(), Citations: cits}
}

func docTitle(e *Engine, s types.Section) string {
	for _, d := range e.R.VerifiedDocs() {
		if d.ID == s.DocID && d.Version == s.Version {
			return d.Title
		}
	}
	return s.DocID
}

// build 模板化组装：事实句 + 逐条出处，不比较文风。
// 输入证据已经过业务日期筛选，不能根据导入时的状态标注历史适用版本。
func (e *Engine) build(evids []evidence) types.Answer {
	var b strings.Builder
	seen := map[string]bool{} // 同父块去重
	var cits []types.Citation
	first := true
	for _, ev := range evids {
		if seen[ev.Sec.ID] {
			continue
		}
		seen[ev.Sec.ID] = true
		if first {
			b.WriteString("根据资料：")
			first = false
		} else {
			b.WriteString("另有规定：")
		}
		b.WriteString(fmt.Sprintf("《%s》%s（%s）：\"%s\"\n",
			docTitle(e, ev.Sec), ev.Sec.Version, ev.Sec.SectionPath, ev.Sentence))
		cits = append(cits, types.Citation{
			DocumentID: ev.Sec.DocID, Version: ev.Sec.Version,
			Location: ev.Sec.SectionPath, Excerpt: ev.Sentence,
		})
	}
	return types.Answer{Status: "answered", Answer: b.String(), Citations: cits}
}

// filterByAsOf excludes documents outside the business date or requested scope.
func (e *Engine) filterByAsOf(hits []types.Hit, asOf *time.Time, scopeKey string) []types.Hit {
	var out []types.Hit
	for _, h := range hits {
		for _, d := range e.R.VerifiedDocs() {
			if d.ID == h.Section.DocID && d.Version == h.Section.Version {
				if d.Status != "needs_review" && d.IdentityKind != "synthetic" && matchesScope(d.ScopeKeys, scopeKey) && e.businessActive(d, *asOf, scopeKey) {
					out = append(out, h)
				}
				break
			}
		}
	}
	return out
}

func matchesScope(keys []string, requested string) bool {
	if requested == "" {
		return true
	}
	for _, key := range keys {
		if key == requested || key == "*" {
			return true
		}
	}
	return false
}

func (e *Engine) weak(query string, hits []types.Hit) bool {
	if len(hits) == 0 {
		return true
	}
	if hits[0].Score < absFloor {
		return true
	}
	// 无任何句子对齐 → 其实没答案（比分数更硬的判据）
	q := tokenize.Expand(tokenize.Tokenize(query))
	for _, h := range hits {
		for _, sent := range splitSentences(h.Section.Text) {
			if alignScore(q, tokenize.Tokenize(sent)) > alignFloor {
				return false
			}
		}
	}
	return true
}

// isModalQuestion 是非问句：以吗/呢结尾或含能不能/可不可以/行不行/是否可以
func isModalQuestion(q string) bool {
	if strings.HasSuffix(q, "吗") || strings.HasSuffix(q, "呢") {
		return true
	}
	for _, p := range []string{"能不能", "可不可以", "行不行", "是否可以", "是否需要"} {
		if strings.Contains(q, p) {
			return true
		}
	}
	return false
}

// hasModalVerdict 证据中是否存在许可/禁止性表述（"须"不算——它不回答允许与否）
func hasModalVerdict(evids []evidence) bool {
	for _, ev := range evids {
		for _, m := range []string{"可以", "不得", "禁止", "允许", "不可", "无需", "无须", "免于", "免收", "免除", "退还"} {
			if strings.Contains(ev.Sentence, m) {
				return true
			}
		}
	}
	return false
}

// needsUnsupportedReasoning rejects operations without a directly stated result.
// These are operator classes, not mappings from individual questions to answers.
func needsUnsupportedReasoning(query string, evids []evidence) bool {
	for _, marker := range []string{"推断", "推测", "预测", "估计"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	calculation := false
	for _, marker := range []string{"合计", "总共", "总额", "加起来", "差额", "平均"} {
		if strings.Contains(query, marker) {
			calculation = true
			break
		}
	}
	if !calculation {
		return false
	}
	for _, ev := range evids {
		for _, marker := range []string{"合计", "总计", "总额", "差额", "平均"} {
			if strings.Contains(ev.Sentence, marker) && hasNumericClaim(ev.Sentence) {
				return false
			}
		}
	}
	return true
}

// splitSentences separates punctuation, steps, and Markdown table rows.
func splitSentences(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if isTableRule(line) || (strings.HasPrefix(strings.TrimSpace(line), "|") && i+1 < len(lines) && isTableRule(lines[i+1])) {
			continue // 表头和分隔行只解释列名，不是事实证据
		}
		var cur strings.Builder
		for _, r := range line {
			cur.WriteRune(r)
			if r == '。' || r == '；' || r == '.' || r == ';' || r == '！' || r == '？' {
				s := strings.TrimSpace(cur.String())
				if s != "" {
					out = append(out, s)
				}
				cur.Reset()
			}
		}
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isTableRule(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return false
	}
	hasDash := false
	for _, r := range line {
		switch r {
		case '|', ' ', '\t', ':':
		case '-':
			hasDash = true
		default:
			return false
		}
	}
	return hasDash
}
