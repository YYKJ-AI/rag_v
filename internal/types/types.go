// Package types 定义全链路核心数据结构。
// 溯源是数据结构问题：所有引用信息在入库时埋好。
package types

import "time"

// Document 文档级打标（对应 front-matter）
type Document struct {
	ID            string    `json:"id"`                       // 稳定 document_id
	Version       string    `json:"version"`                  // v1/v2/...
	Title         string    `json:"title"`
	PublishDate   time.Time `json:"publish_date"`
	EffectiveDate time.Time `json:"effective_date"`           // 业务生效时间
	Scope         string    `json:"scope"`                    // 适用范围
	Nature        string    `json:"nature"`                   // 基础规范/修订通知/补充说明
	Replaces      []string  `json:"replaces"`                 // 明确替代关系 ["art-guide@v1"]
	SupersededBy  string    `json:"superseded_by,omitempty"`  // 反向指针，空=现行
	Fingerprint   uint64    `json:"fingerprint"`              // 文档级 SimHash
	ActivatedAt   time.Time `json:"activated_at"`             // 系统时间轴 valid_from
	SupersededAt  time.Time `json:"superseded_at,omitempty"`  // 系统时间轴 valid_until
	Status        string    `json:"status"`                   // active|superseded|needs_review
}

// Section 父块：引用定位的最小单位
type Section struct {
	ID          string   `json:"id"`           // docId@version#sec-2
	DocID       string   `json:"doc_id"`
	Version     string   `json:"version"`
	SectionPath string   `json:"section_path"` // "第2节 申请条件"（citation 定位符）
	ParaIndex   int      `json:"para_index"`
	Text        string   `json:"text"`         // 父块全文
	Summary     string   `json:"summary"`      // 大纲层检索用（节标题+首句）
	SimHash     uint64   `json:"simhash"`
}

// Chunk 子块：检索的最小单位
type Chunk struct {
	ID        string   `json:"id"`
	SectionID string   `json:"section_id"`
	Text      string   `json:"text"`
	Tokens    []string `json:"tokens"`
	Hash      string   `json:"hash"`     // sha256，精确复用判据
	SimHash   uint64   `json:"simhash"`  // 近重复去重
	// embedding 相关字段预留（向量路径未启用时为空）
	EmbeddingModel   string    `json:"embedding_model,omitempty"`
	EmbeddingVersion string    `json:"embedding_version,omitempty"`
}

// Hit 检索命中
type Hit struct {
	Section Section `json:"section"`
	Chunk   Chunk   `json:"chunk"`
	Score   float64 `json:"score"`
}

// Citation 引用：任务书要求的结构
type Citation struct {
	DocumentID string `json:"document_id"`
	Version    string `json:"version"`
	Location   string `json:"location"`   // "第2节/第3段"
	Excerpt    string `json:"excerpt"`    // 逐字来自父块，防拼接失真
}

// Answer 统一返回结构（任务书规定的形态）
type Answer struct {
	Status    string     `json:"status"`    // answered|conflict|insufficient_evidence
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
}
