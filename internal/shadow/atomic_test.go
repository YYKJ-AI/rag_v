package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/evaluate"
	"ragbot/internal/generator"
)

func TestG08RequiresResolvedReferent(t *testing.T) {
	labels, err := LoadAtomicCases("../../eval/atomic-shadow.json")
	if err != nil {
		t.Fatal(err)
	}
	var label AtomicCase
	for _, tc := range labels {
		if tc.ID == "G08" {
			label = tc
		}
	}
	if len(label.Requirements) != 1 || len(label.Requirements[0].EvidenceSets) != 2 {
		t.Fatalf("LoadAtomicCases(G08) = %+v, want one requirement with two alternative evidence sets", label)
	}
	main := engine.Candidate{ID: "main", DocumentID: "bluebridge-venue", Version: "v1", Location: "第2节 场地报备", Text: "预计人数超过四十人的场次，须增加一名现场疏导员。"}
	alternative := engine.Candidate{ID: "alternative", DocumentID: "bluebridge-venue", Version: "v1", Location: "第4节 人员与物料", Text: "超过四十人时，另安排现场疏导员。"}
	exception := engine.Candidate{ID: "exception", DocumentID: "bluebridge-venue", Version: "v1", Location: "第2节 场地报备", Text: "人数恰好四十人的场次不触发该附加要求。"}
	for _, tc := range []struct {
		name       string
		candidates []engine.Candidate
		want       bool
	}{
		{"isolated exception", []engine.Candidate{exception}, false},
		{"same section referent", []engine.Candidate{main, exception}, true},
		{"other section referent", []engine.Candidate{alternative, exception}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goldComplete(tc.candidates, nil, label); got != tc.want {
				t.Errorf("goldComplete(G08, %s) = %t, want %t", tc.name, got, tc.want)
			}
		})
	}
}

func TestLoadAtomicCasesRejectsEmptyEvidenceGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	data := `[{"id":"bad","query":"费用是多少","expect_status":"answered","requirements":[{"id":"fee","requirement":"报名费用","evidence_sets":[[]]}]}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadAtomicCases(path); err == nil {
		t.Errorf("LoadAtomicCases(empty evidence set) = %+v, nil; want validation error", got)
	}
}

func TestEvidenceSetsANDORAndWrongAction(t *testing.T) {
	ev := func(contains string) evaluate.Evidence {
		return evaluate.Evidence{DocumentID: "guide", Version: "v1", Location: "规则", ExcerptContains: contains}
	}
	label := AtomicCase{ExpectStatus: "answered", Requirements: []AtomicLabel{{ID: "a", Requirement: "费用", EvidenceSets: [][]evaluate.Evidence{{ev("费用一"), ev("费用二")}, {ev("费用替代")}}}}}
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"one of AND missing", "费用一", false}, {"both AND", "费用一、费用二", true}, {"OR alternative", "费用替代", true}, {"different action", "上传证件照", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := goldComplete([]engine.Candidate{{ID: "x", DocumentID: "guide", Version: "v1", Location: "规则", Text: tc.text}}, nil, label)
			if got != tc.want {
				t.Errorf("goldComplete(%q) = %t, want %t", tc.text, got, tc.want)
			}
		})
	}
}

func TestCompareAtomicReplaysOldPoolAndSeparatesMisses(t *testing.T) {
	candidates := []engine.Candidate{
		{ID: "a", DocumentID: "guide", Version: "v1", Location: "规则", Text: "先报备"},
		{ID: "b", DocumentID: "guide", Version: "v1", Location: "规则", Text: "另办许可"},
	}
	label := AtomicCase{ID: "q", Query: "需要什么手续", ExpectStatus: "answered", Requirements: []AtomicLabel{
		{ID: "report", Requirement: "报备", EvidenceSets: [][]evaluate.Evidence{{{DocumentID: "guide", Version: "v1", Location: "规则", ExcerptContains: "先报备"}}}},
		{ID: "permit", Requirement: "许可", EvidenceSets: [][]evaluate.Evidence{{{DocumentID: "guide", Version: "v1", Location: "规则", ExcerptContains: "另办许可"}}}},
	}}
	report := Report{Records: []Record{{ID: "q", Query: label.Query, ExpectedStatus: "answered", Candidates: candidates, Decision: &Decision{SupportedIDs: []string{"a", "b"}, Sufficient: false, Reason: "不充分"}}}}
	judge := fixedAtomicJudge{decision: AtomicDecision{Requirements: []AtomicSupport{
		{Requirement: AtomicRequirement{Subject: "活动", Action: "办理", Object: "报备", Modality: "必须"}, SupportedIDs: []string{"a"}, Supported: true},
		{Requirement: AtomicRequirement{Subject: "活动", Action: "办理", Object: "许可", Modality: "必须"}, SupportedIDs: []string{"b"}, Supported: true},
	}, SupportedIDs: []string{"a", "b"}, Sufficient: true, Calls: 3}}
	got, err := CompareAtomic(context.Background(), []Report{report}, []AtomicCase{label}, judge)
	if err != nil {
		t.Fatal(err)
	}
	if got.OldMisses != 1 || got.NewMisses != 0 || got.NewUnsafe != 0 || got.ModelCalls != 3 || got.DecompositionRecall != "pending_manual_review" {
		t.Errorf("CompareAtomic(two requirements) = old misses %d, new misses %d, unsafe %d, calls %d, review %q; want 1, 0, 0, 3, pending_manual_review", got.OldMisses, got.NewMisses, got.NewUnsafe, got.ModelCalls, got.DecompositionRecall)
	}
	if got.Records[0].New == nil || !got.Records[0].New.EvidenceComplete {
		t.Errorf("CompareAtomic(two requirements).Records[0].New = %+v, want complete evidence", got.Records[0].New)
	}
	bad := judge
	bad.decision.Requirements[1].SupportedIDs = []string{"invented"}
	bad.decision.SupportedIDs = []string{"a", "invented"}
	got, err = CompareAtomic(context.Background(), []Report{report}, []AtomicCase{label}, bad)
	if err != nil || got.ModelErrors != 1 || got.NewValid != 0 || got.Records[0].Error == "" {
		t.Errorf("CompareAtomic(invented ID) = errors %d, valid %d, err %v; want 1, 0, nil", got.ModelErrors, got.NewValid, err)
	}
}

type fixedAtomicJudge struct{ decision AtomicDecision }

func (j fixedAtomicJudge) JudgeAtomic(context.Context, string, []engine.Candidate) (AtomicDecision, error) {
	return j.decision, nil
}

type failedAtomicJudge struct{}

func (failedAtomicJudge) JudgeAtomic(context.Context, string, []engine.Candidate) (AtomicDecision, error) {
	return AtomicDecision{Calls: 2, LatencyMs: 17, Decomposed: []AtomicRequirement{{Subject: "活动", Action: "收取", Object: "费用", Modality: "数值"}}}, fmt.Errorf("invalid model response")
}

func TestCompareAtomicCountsFailedCalls(t *testing.T) {
	label := AtomicCase{ID: "q", Query: "费用是多少", ExpectStatus: "insufficient_evidence", Requirements: []AtomicLabel{{ID: "fee", Requirement: "费用数值"}}}
	report := Report{Records: []Record{{ID: "q", Query: label.Query, ExpectedStatus: label.ExpectStatus, Candidates: []engine.Candidate{{ID: "a", Text: "费用另行公布"}}}}}
	got, err := CompareAtomic(context.Background(), []Report{report}, []AtomicCase{label}, failedAtomicJudge{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelCalls != 2 || got.TotalModelLatencyMs != 17 || got.ModelErrors != 1 || got.NewValid != 0 {
		t.Errorf("CompareAtomic(failed judge) = calls %d, latency %v, errors %d, valid %d; want 2, 17, 1, 0", got.ModelCalls, got.TotalModelLatencyMs, got.ModelErrors, got.NewValid)
	}
	if got.Records[0].AtomicDecision == nil || len(got.Records[0].AtomicDecision.Decomposed) != 1 {
		t.Errorf("CompareAtomic(failed judge).Records[0].AtomicDecision = %+v, want retained decomposition", got.Records[0].AtomicDecision)
	}
}

func TestRemoteAtomicRejectsUnknownIDsAndIncompleteSchema(t *testing.T) {
	candidate := engine.Candidate{ID: "a", DocumentID: "guide", Version: "v1", Location: "规则", Text: "年满十二周岁"}
	for _, tc := range []struct {
		name, support string
		wantErr       bool
	}{
		{"valid", `{"supported":true,"supported_ids":["a"],"reason":"年龄有据"}`, false},
		{"unknown ID", `{"supported":true,"supported_ids":["fake"],"reason":"有据"}`, true},
		{"missing IDs", `{"reason":"无据"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				content := `{"requirements":[{"subject":"参加者","action":"参加","object":"工作坊","condition":"","modality":"最低年龄"}]}`
				if calls == 2 {
					content = tc.support
				}
				_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
			}))
			defer server.Close()
			judge, err := NewRemoteJudge(generator.Config{Enabled: true, Provider: "openai_compatible", BaseURL: server.URL, Model: "test", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			got, err := judge.JudgeAtomic(context.Background(), "最低年龄", []engine.Candidate{candidate})
			if (err != nil) != tc.wantErr {
				t.Errorf("JudgeAtomic(%s) error = %v, want error presence %t", tc.name, err, tc.wantErr)
			}
			if !tc.wantErr && (!got.Sufficient || got.Calls != 2 || len(got.SupportedIDs) != 1) {
				t.Errorf("JudgeAtomic(%s) = %+v, want one supported ID and two calls", tc.name, got)
			}
			if calls != 2 {
				t.Errorf("JudgeAtomic(%s) HTTP calls = %d, want 2", tc.name, calls)
			}
		})
	}
}

func TestAtomicPromptNeverReceivesGoldLabels(t *testing.T) {
	// Reported gold is scored after the model call. The request only contains the
	// question, a blind requirement, and source candidates.
	var payloads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		data, _ := json.Marshal(request)
		payloads = append(payloads, string(data))
		content := `{"requirements":[{"subject":"活动","action":"满足","object":"年龄","modality":"下限","condition":""}]}`
		if len(payloads) == 2 {
			content = `{"supported":true,"supported_ids":["a"],"reason":"有据"}`
		}
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer server.Close()
	judge, err := NewRemoteJudge(generator.Config{Enabled: true, Provider: "openai_compatible", BaseURL: server.URL, Model: "test", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = judge.JudgeAtomic(context.Background(), "年龄", []engine.Candidate{{ID: "a", Text: "年满十二周岁"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range payloads {
		if strings.Contains(payload, "evidence_sets") || strings.Contains(payload, "expect_status") {
			t.Errorf("JudgeAtomic(age) request contains gold-label fields, want none")
		}
	}
}
