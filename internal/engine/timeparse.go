// 查询时间点解析：从问句文本推断 as_of（确定性解析器，非硬关键词词表）。
// 用户不选日期——"2025年申请要交几项材料"自动切 2025 口径，"现在…"走现行。
package engine

import (
	"regexp"
	"strconv"
	"time"
)

var (
	reFullDate  = regexp.MustCompile(`(20\d{2})年(\d{1,2})月(\d{1,2})日`)
	reYearMonth = regexp.MustCompile(`(20\d{2})年(\d{1,2})月`)
	reYear      = regexp.MustCompile(`(20\d{2})年`)
	reISODate   = regexp.MustCompile(`\b(20\d{2})-(\d{1,2})-(\d{1,2})\b`)
	reLastYear  = regexp.MustCompile(`去年|上一年度`)
)

// QueryHasInferredTime reports whether the question contains a valid time point.
// It is pure and safe to use for a single HTTP request's response metadata.
func QueryHasInferredTime(query string) bool { return inferQueryTime(query) != nil }

// inferQueryTime 从问句推断时间点；无时间表达 → nil（走现行口径）。
// 解析失败一律回退 nil，不报错——时间推断是增强不是门槛。
func inferQueryTime(q string) *time.Time {
	if m := reFullDate.FindStringSubmatch(q); m != nil {
		return parseYMD(m[1], m[2], m[3])
	}
	if m := reISODate.FindStringSubmatch(q); m != nil {
		return parseYMD(m[1], m[2], m[3])
	}
	if m := reYearMonth.FindStringSubmatch(q); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if mo >= 1 && mo <= 12 {
			t := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1) // 月末（含当月）
			return &t
		}
		return nil
	}
	if m := reYear.FindStringSubmatch(q); m != nil {
		y, _ := strconv.Atoi(m[1])
		t := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC) // 年末（含全年）
		return &t
	}
	now := time.Now()
	if reLastYear.MatchString(q) {
		t := time.Date(now.Year()-1, 12, 31, 0, 0, 0, 0, time.UTC)
		return &t
	}
	return nil
}

func parseYMD(ys, ms, ds string) *time.Time {
	y, err1 := strconv.Atoi(ys)
	mo, err2 := strconv.Atoi(ms)
	d, err3 := strconv.Atoi(ds)
	if err1 != nil || err2 != nil || err3 != nil || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return nil
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return nil
	}
	return &t
}

// InferQueryTime 导出版时间推断（正则优先路径）：查询含确定性时间表达时
// 返回业务锚点（期末含全期约定），供服务端优先于模型提取使用——
// 模型在相对时间上已两次实测翻车（猜错年份/精度），确定性解析覆盖的表达式不交给模型。
func InferQueryTime(query string) *time.Time { return inferQueryTime(query) }
