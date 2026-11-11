package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var versionHintPattern = regexp.MustCompile(`(?i)\bv[0-9]+(?:\.[0-9]+)*\b|第[一二三四五六七八九十0-9]+版|[0-9]{4}年版`)

// ParseUnstructured prepares a nonstandard Markdown file for review-only indexing.
// Explicitly malformed metadata is still an error; inferred values never become business facts.
func ParseUnstructured(raw []byte, sourceName string) (*DocMeta, []SectionDraft, error) {
	sourceName = filepath.Base(sourceName)
	if sourceName == "." || sourceName == "" {
		return nil, nil, fmt.Errorf("缺少来源文件名")
	}
	body := strings.TrimPrefix(string(raw), "\uFEFF")
	var declaredTitle string
	warnings := []string{"metadata_incomplete", "business_date_unknown"}
	if strings.HasPrefix(body, "---") {
		end := strings.Index(body[3:], "\n---")
		if end < 0 {
			return nil, nil, fmt.Errorf("front-matter 未闭合")
		}
		fm := body[3 : 3+end]
		body = body[3+end+4:]
		if i := strings.Index(body, "\n"); i >= 0 {
			body = body[i+1:]
		}
		for _, line := range strings.Split(fm, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, nil, fmt.Errorf("front-matter 行格式无效：%q", line)
			}
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "title":
				declaredTitle = value
			case "effective_date", "publish_date", "valid_until":
				if value != "" {
					if _, err := parseDate(value); err != nil {
						return nil, nil, fmt.Errorf("%s 格式应为 YYYY-MM-DD: %w", key, err)
					}
				}
			}
		}
	} else {
		warnings = append(warnings, "missing_front_matter")
	}
	drafts, err := parseBody(body)
	if err != nil {
		return nil, nil, err
	}
	hash := sha256.Sum256(raw)
	contentHash := hex.EncodeToString(hash[:])
	title := declaredTitle
	if title == "" {
		title = strings.TrimSuffix(sourceName, filepath.Ext(sourceName))
		warnings = append(warnings, "title_from_filename")
	}
	var hint string
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if i >= 8 {
			break
		}
		if found := versionHintPattern.FindString(line); found != "" {
			hint = found
			break
		}
	}
	return &DocMeta{
		ID: "synthetic_" + contentHash[:12], Version: "unversioned", Title: title,
		ContentHash: contentHash, SourceName: sourceName, IdentityKind: "synthetic",
		MetadataWarnings: warnings, VersionHint: hint,
	}, drafts, nil
}
