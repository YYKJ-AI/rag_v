package engine

import (
	"fmt"
	"strings"

	"ragbot/internal/safety"
)

// Candidate is an unchanged source sentence eligible for a shadow judgment.
// Its ID is stable for a fixed index snapshot; it is not a persistent citation ID.
type Candidate struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Version    string `json:"version"`
	Location   string `json:"location"`
	Text       string `json:"text"`
}

// CandidateSentences returns sentences from the first eligible parent sections.
// It deliberately bypasses the lexical sentence threshold used by Answer.
func (e *Engine) CandidateSentences(query string, maxSections int) []Candidate {
	if strings.TrimSpace(query) == "" || safety.IsDirectOverride(query) {
		return nil
	}
	var out []Candidate
	for _, hit := range e.Top(query, maxSections) {
		for i, sentence := range splitSentences(hit.Section.Text) {
			out = append(out, Candidate{
				ID:         fmt.Sprintf("%s/sent-%d", hit.Section.ID, i+1),
				DocumentID: hit.Section.DocID, Version: hit.Section.Version,
				Location: hit.Section.SectionPath, Text: sentence,
			})
		}
	}
	return out
}
