package generator

import (
	"fmt"
	"regexp"
	"strings"

	"ragbot/internal/safety"
)

var (
	arabicNumber    = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	chineseQuantity = regexp.MustCompile(`[零〇一二三四五六七八九十百千万两]+(?:个|项|元|人|岁|周岁|天|日|月|年|次|小时|工作日)`)
	latinIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_-]{2,}`)
	sourceName      = regexp.MustCompile(`《[^》]+》`)
	listOrdinal     = regexp.MustCompile(`(?m)^\s*[0-9]+[.、)]\s*`)
	versionToken    = regexp.MustCompile(`(?i)v\d+(?:\.\d+)?`)
)

// ValidateRewrite checks mechanical facts before an LLM rewrite is shown.
// It is a conservative guard, not a semantic proof of support.
func (g *Gateway) ValidateRewrite(baseAnswer string, evidence []string, output string) error {
	output = strings.TrimSpace(output)
	if output == "" || len(output) > 16<<10 {
		return fmt.Errorf("模型表述为空或过长")
	}
	if len(g.cfg.APIKey) >= 8 && strings.Contains(output, g.cfg.APIKey) {
		return fmt.Errorf("模型表述包含凭证")
	}
	if safety.IsDirectOverride(output) {
		return fmt.Errorf("模型表述包含指令样式文本")
	}
	for _, refusal := range []string{"证据不足", "无法回答", "无法确定", "无法给出有出处的回答"} {
		if strings.Contains(output, refusal) && !strings.Contains(baseAnswer, refusal) {
			return fmt.Errorf("模型表述改变了回答状态")
		}
	}
	evidenceText := strings.Join(evidence, "\n")
	if strings.TrimSpace(evidenceText) == "" {
		return fmt.Errorf("模型表述缺少可核验的引用原文")
	}
	// 数字保留校验只针对"回答原文"中的数字——那是答案的事实主张。
	// 证据摘录中的其余数字是背景材料，模型合法摘要时允许省略；
	// 强制保留会让每次摘要式改写都失败（2026-11-13 实测回退率过高的根因）。
	// 版本标记（v1/v2）由 citations 面板展示，不要求改写复述。
	factsBase := versionToken.ReplaceAllString(baseAnswer, "")
	checkText := listOrdinal.ReplaceAllString(output, "")
	facts := baseAnswer + "\n" + evidenceText
	for _, pattern := range []*regexp.Regexp{arabicNumber, chineseQuantity} {
		for _, token := range pattern.FindAllString(factsBase, -1) {
			if !strings.Contains(output, token) {
				return fmt.Errorf("模型表述删除了既有回答的数字或数量")
			}
		}
	}
	// 输出中出现的新数字/标识仍必须能在回答或证据中找到（防编造）。
	for _, pattern := range []*regexp.Regexp{sourceName, arabicNumber, chineseQuantity, latinIdentifier} {
		for _, token := range pattern.FindAllString(checkText, -1) {
			if !strings.Contains(strings.ToLower(facts), strings.ToLower(token)) {
				return fmt.Errorf("模型表述包含未给出的来源、数字或标识")
			}
		}
	}
	return nil
}
