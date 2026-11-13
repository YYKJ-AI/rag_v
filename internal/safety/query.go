// Package safety keeps narrow checks for direct instruction override attempts.
// It cannot determine whether arbitrary retrieved text is semantically safe.
package safety

import "strings"

// IsDirectOverride catches explicit requests to replace instructions or
// disclose runtime secrets. Quoted discussion of such text remains searchable.
func IsDirectOverride(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || isDiscussionOfInstructions(q) {
		return false
	}
	controls := []string{"忽略之前", "忽略以上", "忽略检索", "无视之前", "绕过系统", "覆盖系统"}
	secrets := []string{"系统提示词", "系统指令", "环境变量", "密钥", "api key", "apikey"}
	commands := []string{"输出", "显示", "泄露", "读取", "告诉", "直接回答", "打印"}
	if containsAny(q, controls) {
		return true
	}
	return containsAny(q, secrets) && containsAny(q, commands)
}

func isDiscussionOfInstructions(query string) bool {
	context := []string{"文档中", "资料中", "原文中", "这句话", "这段话", "这段文字"}
	if !startsAny(query, context) {
		return false
	}
	// A discussion question followed by a new imperative is still an override.
	q := strings.TrimRight(query, "？?。！! ")
	for _, ending := range []string{"什么意思", "是什么意思", "是否出现", "有没有", "如何理解", "怎么处理"} {
		if strings.HasSuffix(q, ending) {
			for _, pair := range [][2]string{{"“", "”"}, {"\"", "\""}, {"‘", "’"}} {
				start := strings.Index(q, pair[0])
				if start < 0 {
					continue
				}
				end := strings.Index(q[start+len(pair[0]):], pair[1])
				if end < 0 {
					continue
				}
				end += start + len(pair[0]) + len(pair[1])
				outside := q[:start] + q[end:]
				return !IsDirectOverride(outside)
			}
		}
	}
	return false
}

func startsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.HasPrefix(text, phrase) {
			return true
		}
	}
	return false
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
