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
	// 数字校验（2026-11-13 二次修正）：
	// 1) 保留校验从"全部保留"放宽为"至少保留一个"——多引用模板答案含大量背景数字，
	//    强制全保留使摘要式改写必然失败（LLM 路径结构性不可用的根因）；
	// 2) 编造校验保留：改写中新出现的数字/来源必须可溯源。
	// 版本标记（v1/v2）由 citations 面板展示，不参与校验。
	factsBase := versionToken.ReplaceAllString(baseAnswer, "")
	checkText := listOrdinal.ReplaceAllString(output, "")
	facts := baseAnswer + "\n" + evidenceText
	var baseNumbers []string
	for _, pattern := range []*regexp.Regexp{arabicNumber, chineseQuantity} {
		baseNumbers = append(baseNumbers, pattern.FindAllString(factsBase, -1)...)
	}
	if len(baseNumbers) > 0 {
		kept := false
		for _, token := range baseNumbers {
			if strings.Contains(output, token) {
				kept = true
				break
			}
		}
		if !kept {
			return fmt.Errorf("模型表述未保留任何关键数值，存在空泛化风险")
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
