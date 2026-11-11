// Package store 存储层：内存 + JSON 文件持久化（默认路径，零外部依赖）。
// PG 适配器为演化位（Retriever/Store 接口已隔离）；本文件保证验收路径零环境依赖。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"ragbot/internal/types"
)

// Store 全量数据的内存态 + 落盘
type Store struct {
	mu       sync.RWMutex
	Docs     []*types.Document
	Sections []*types.Section
	Chunks   []*types.Chunk
	Log      []IngestEvent
	path     string
}

// IngestEvent 幂等审计 + outbox 形态预留
type IngestEvent struct {
	Fingerprint uint64    `json:"fingerprint"`
	DocID       string    `json:"doc_id"`
	Version     string    `json:"version"`
	Action      string    `json:"action"`
	ImportedAt  time.Time `json:"imported_at"`
}

func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 首次运行
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(b, s)
}

func (s *Store) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Ingest 导入一份文档（幂等三判：完全重复 / 版本演进 / 新文档）
func (s *Store) Ingest(doc *types.Document, secs []*types.Section, chunks []*types.Chunk) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := doc.ID + "@" + doc.Version
	for _, d := range s.Docs {
		if d.ID == doc.ID && d.Version == doc.Version {
			if doc.IdentityKind == "synthetic" && d.ContentHash != doc.ContentHash {
				return "", fmt.Errorf("合成 ID 碰撞：%s 对应不同内容", key)
			}
			if (doc.IdentityKind == "synthetic" && d.ContentHash == doc.ContentHash) || (doc.IdentityKind != "synthetic" && d.Fingerprint == doc.Fingerprint) {
				if doc.IdentityKind == "synthetic" && doc.SourceName != d.SourceName && !containsName(d.Aliases, doc.SourceName) {
					d.Aliases = append(d.Aliases, doc.SourceName)
				}
				s.logLocked(doc, "duplicate")
				return "duplicate", nil // 重复导入同版本 → 幂等跳过
			}
			// 同 ID 同版本不同内容 → 拒绝，要求升版本号
			return "", fmt.Errorf("版本冲突：%s 已存在且内容不同，请升版本号后重新导入", key)
		}
	}
	if doc.IdentityKind == "synthetic" {
		for _, d := range s.Docs {
			if d.IdentityKind == "synthetic" && (d.SourceName == doc.SourceName || containsName(d.Aliases, doc.SourceName)) && d.ContentHash != doc.ContentHash {
				doc.MetadataWarnings = append(doc.MetadataWarnings, "possible_duplicate_name")
				break
			}
		}
	}

	// 替代关系生效：replaces 指向的旧版本关窗
	for _, r := range doc.Replaces {
		base, ver := r, ""
		if i := strings.IndexByte(r, '@'); i >= 0 {
			base, ver = r[:i], r[i+1:]
		}
		for _, d := range s.Docs {
			if d.ID != base {
				continue
			}
			if ver != "" && d.Version != ver {
				continue
			}
			d.Status = "superseded"
			d.SupersededBy = doc.ID + "@" + doc.Version
			if d.SupersededAt.IsZero() {
				d.SupersededAt = time.Now()
			}
		}
	}

	s.Docs = append(s.Docs, doc)
	s.Sections = append(s.Sections, secs...)
	s.Chunks = append(s.Chunks, chunks...)
	s.logLocked(doc, "created")
	return "created", nil
}

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

func (s *Store) logLocked(doc *types.Document, action string) {
	s.Log = append(s.Log, IngestEvent{
		Fingerprint: doc.Fingerprint, DocID: doc.ID, Version: doc.Version,
		Action: action, ImportedAt: time.Now(),
	})
}

func (s *Store) Events() []IngestEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]IngestEvent{}, s.Log...)
}
