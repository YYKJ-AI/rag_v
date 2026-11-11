// Package tokenize 中文分词（二元 bigram + 拉丁词）与同义词表展开。
// 纯标准库实现，零外部依赖；演化期可替换为 gse/jieba 词典分词（Tokenizer 接口已隔离）。
package tokenize

import (
	"strings"
	"unicode"
)

// Synonyms 同义词表：同义表达问题的第一道兜底。
// 从评测集失败案例反向补充；只做检索侧展开，不改用户原句存证。
var Synonyms = map[string][]string{
	"报名": {"申请", "申报"},
	"申请": {"报名", "申报"},
	"资格": {"条件", "要求", "申请人"},
	"条件": {"要求", "资格", "申请人"},
	"要求": {"条件", "规定"},
	"规定": {"要求", "条件"},
	"费用": {"收费", "价格", "费率"},
	"收费": {"费用", "价格"},
	"价格": {"费用", "收费"},
	"时间": {"日期", "期限"},
	"时限": {"期限", "工作日"},
	"截止": {"截止日期", "最后"},
	"场地": {"场所", "场地使用"},
	"演出": {"表演", "节目"},
	"材料": {"资料", "文件"},
	"审批": {"审核", "批准"},
}

// Tokenize 分词：中文按二元 bigram，拉丁字母/数字按词。
// bigram 对 BM25 召回质量足够，且天然免疫未登录词。
func Tokenize(text string) []string {
	text = strings.ToLower(text)
	var toks []string
	var curWord strings.Builder
	var curHan strings.Builder

	flushWord := func() {
		if curWord.Len() > 0 {
			toks = append(toks, curWord.String())
			curWord.Reset()
		}
	}
	flushHan := func() {
		s := curHan.String()
		curHan.Reset()
		runes := []rune(s)
		// 同时输出单字与二元 bigram：bigram 提精度，unigram 保召回
		for _, r := range runes {
			toks = append(toks, string(r))
		}
		for i := 0; i+2 <= len(runes); i++ {
			toks = append(toks, string(runes[i:i+2]))
		}
	}

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			curHan.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			curWord.WriteRune(r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return toks
}

// Expand 同义词表展开：原词 + 同义词（去重，保持确定性顺序）
func Expand(toks []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range toks {
		add(t)
		if syns, ok := Synonyms[t]; ok {
			for _, s := range syns {
				add(s)
			}
		}
	}
	return out
}

// Jaccard 词级 Jaccard 相似度（expand 后比较），用于句子对齐兜底
func Jaccard(a, b []string) float64 {
	sa := map[string]bool{}
	for _, t := range a {
		sa[t] = true
	}
	inter := 0
	for _, t := range b {
		if sa[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
