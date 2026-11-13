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
	facts := baseAnswer + "\n" + evidenceText
	checkText := listOrdinal.ReplaceAllString(output, "")
	for _, pattern := range []*regexp.Regexp{arabicNumber, chineseQuantity} {
		// Version numbers in the template header remain in immutable citations.
		// Only quantities in the cited source text must survive the rewrite.
		for _, token := range pattern.FindAllString(evidenceText, -1) {
			if !strings.Contains(output, token) {
				return fmt.Errorf("模型表述删除了既有回答的数字或数量")
			}
		}
	}
	for _, pattern := range []*regexp.Regexp{sourceName, arabicNumber, chineseQuantity, latinIdentifier} {
		for _, token := range pattern.FindAllString(checkText, -1) {
			if !strings.Contains(strings.ToLower(facts), strings.ToLower(token)) {
				return fmt.Errorf("模型表述包含未给出的来源、数字或标识")
			}
		}
	}
	return nil
}
