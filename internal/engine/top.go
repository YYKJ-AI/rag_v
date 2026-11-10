// Package engine 导出 Top 供评测 Hit@5 判定（与 Answer 共用同一检索路径）。
package engine

import "ragbot/internal/types"

// Top 检索 topN（评测 Hit@5 用，确定性同 Answer 内部路径）
func (e *Engine) Top(query string, n int) []types.Hit {
	return e.R.Search(query, n)
}
