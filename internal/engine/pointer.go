package engine

import (
	"fmt"
	"strings"
	"time"

	"ragbot/internal/types"
)

// answerFromPointer follows only an explicit, current, single-hop inheritance
// edge. The referenced historical section is evidence, not a current document.
func (e *Engine) answerFromPointer(query string, evids []evidence, asOf time.Time, scopeKey string) (types.Answer, bool) {
	var selected *evidence
	for _, ev := range evids {
		if !isPointerEvidence(ev) {
			continue
		}
		if selected != nil && selected.Chunk.ID != ev.Chunk.ID {
			return pointerRefusal(), true
		}
		candidate := ev
		selected = &candidate
	}
	if selected == nil {
		return types.Answer{}, false
	}
	ev := *selected
	if len(ev.Chunk.Relations) != 1 {
		return pointerRefusal(), true
	}
	relation := ev.Chunk.Relations[0]
	if relation.Status != "parsed" || relation.SourceChunkID != ev.Chunk.ID {
		return pointerRefusal(), true
	}
	if relation.Type == types.RelationReferences {
		target, ok := e.lookupPointerTarget(relation, ev.Sec, asOf, scopeKey)
		if !ok || (!strings.Contains(query, "参照") && !strings.Contains(query, "引用")) {
			return pointerRefusal(), true
		}
		return types.Answer{
			Status: "answered",
			Answer: fmt.Sprintf("《%s》%s（%s）声明：%s\n所指向的是《%s》%s（%s）；该引用本身不表示沿用目标条款的具体条件。",
				docTitle(e, ev.Sec), ev.Sec.Version, ev.Sec.SectionPath, relation.RawPointer,
				docTitle(e, target), target.Version, target.SectionPath),
			Citations: []types.Citation{{DocumentID: ev.Sec.DocID, Version: ev.Sec.Version, Location: ev.Sec.SectionPath, Excerpt: relation.RawPointer, Role: "current_pointer"}},
		}, true
	}
	if relation.Type != types.RelationInherits {
		return pointerRefusal(), true
	}
	target, ok := e.resolveInheritedClause(ev.Chunk, ev.Sec, asOf, scopeKey)
	if !ok {
		return pointerRefusal(), true
	}
	return types.Answer{
		Status: "answered",
		Answer: fmt.Sprintf("现行来源《%s》%s（%s）声明：%s\n被引用条款《%s》%s（%s）规定：%s\n具体条款来自 %s，由 %s 声明沿用。",
			docTitle(e, ev.Sec), ev.Sec.Version, ev.Sec.SectionPath, relation.RawPointer,
			docTitle(e, target), target.Version, target.SectionPath, target.Text,
			target.Version, ev.Sec.Version),
		Citations: []types.Citation{
			{DocumentID: ev.Sec.DocID, Version: ev.Sec.Version, Location: ev.Sec.SectionPath, Excerpt: relation.RawPointer, Role: "current_pointer"},
			{DocumentID: target.DocID, Version: target.Version, Location: target.SectionPath, Excerpt: target.Text, Role: "inherited_clause"},
		},
	}, true
}

func isPointerEvidence(ev evidence) bool {
	if ev.Chunk.Kind != "pointer" || len(ev.Chunk.Relations) != 1 {
		return false
	}
	for _, marker := range []string{"沿用", "引用", "参照"} {
		if strings.Contains(ev.Sentence, marker) && strings.Contains(ev.Chunk.Relations[0].RawPointer, ev.Sentence) {
			return true
		}
	}
	return false
}

func withoutPointerDeclarations(evids []evidence) []evidence {
	var facts []evidence
	for _, ev := range evids {
		if !isPointerEvidence(ev) {
			facts = append(facts, ev)
		}
	}
	return facts
}

func pointerRefusal() types.Answer {
	return types.Answer{
		Status: "insufficient_evidence",
		Answer: "找到当前版本的指针声明，但其目标条款无法唯一核验，不能据此回答具体条件。",
	}
}

func (e *Engine) resolveInheritedClause(ptr types.Chunk, source types.Section, asOf time.Time, scopeKey string) (types.Section, bool) {
	if len(ptr.Relations) != 1 || ptr.Relations[0].Type != types.RelationInherits {
		return types.Section{}, false
	}
	relation := ptr.Relations[0]
	target, found := e.lookupPointerTarget(relation, source, asOf, scopeKey)
	if !found {
		return types.Section{}, false
	}
	// V1 deliberately stops after one hop, even if the target has another pointer.
	for _, marker := range []string{"沿用", "引用", "参照"} {
		if strings.Contains(target.Text, marker) {
			return types.Section{}, false
		}
	}
	return target, true
}

func (e *Engine) lookupPointerTarget(relation types.Relation, source types.Section, asOf time.Time, scopeKey string) (types.Section, bool) {
	if relation.Status != "parsed" || relation.TargetSectionID == source.ID || relation.TargetDocID == "" || relation.TargetVersion == "" {
		return types.Section{}, false
	}
	var sourceDoc, targetDoc *types.Document
	for _, doc := range e.R.VerifiedDocs() {
		if doc.ID == source.DocID && doc.Version == source.Version {
			sourceDoc = doc
		}
		if doc.ID == relation.TargetDocID && doc.Version == relation.TargetVersion {
			targetDoc = doc
		}
	}
	if sourceDoc == nil || targetDoc == nil || sourceDoc.Status == "needs_review" || targetDoc.Status == "needs_review" || sourceDoc.IdentityKind == "synthetic" || targetDoc.IdentityKind == "synthetic" || targetDoc.EffectiveDate.After(sourceDoc.EffectiveDate) || targetDoc.EffectiveDate.After(asOf) || !matchesScope(targetDoc.ScopeKeys, scopeKey) || !pointerScopesOverlap(sourceDoc.ScopeKeys, targetDoc.ScopeKeys) {
		return types.Section{}, false
	}
	if !targetDoc.ValidUntil.IsZero() && targetDoc.ValidUntil.Before(sourceDoc.EffectiveDate) {
		return types.Section{}, false
	}
	target, found := e.R.Section(relation.TargetSectionID)
	if !found || target.DocID != relation.TargetDocID || target.Version != relation.TargetVersion || target.Text == "" {
		return types.Section{}, false
	}
	return target, true
}

func pointerScopesOverlap(source, target []string) bool {
	if len(source) == 0 || len(target) == 0 {
		return false
	}
	for _, from := range source {
		for _, to := range target {
			if from == to || from == "*" || to == "*" {
				return true
			}
		}
	}
	return false
}
