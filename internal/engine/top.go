// Package engine 导出 Top 供评测 Hit@5 判定（与 Answer 共用业务时间筛选）。
package engine

import (
	"time"

	"ragbot/internal/types"
)

// Top returns the first eligible parent hits for the query's business date.
func (e *Engine) Top(query string, n int) []types.Hit {
	asOf := inferQueryTime(query)
	if asOf == nil {
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		asOf = &today
	}
	hits := e.filterByAsOf(e.R.SearchVerified(query, 0), asOf, "")
	if n > 0 && len(hits) > n {
		return hits[:n]
	}
	return hits
}
