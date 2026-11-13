package engine_test

import (
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/retriever"
	"ragbot/internal/types"
)

// TestAnswerWithScopeSubExpandsCandidates 子问题候选扩充：
// 原问题只召回场地费用；子问题补充报名费候选，合并后两处证据都进回答。
func TestAnswerWithScopeSubExpandsCandidates(t *testing.T) {
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	secA := &types.Section{ID: "venue@v1#s1", DocID: "venue", Version: "v1", SectionPath: "场地费用",
		Text: "活动场租费用为每天三千元，布展当日减半。"}
	secB := &types.Section{ID: "signup@v1#s1", DocID: "signup", Version: "v1", SectionPath: "报名费用",
		Text: "报名费为一百元，包含活动材料费。"}
	r := retriever.New()
	r.Index(
		[]*types.Document{
			{ID: "venue", Version: "v1", Title: "场地费用标准", EffectiveDate: date},
			{ID: "signup", Version: "v1", Title: "报名收费说明", EffectiveDate: date},
		},
		[]*types.Section{secA, secB},
		[]*types.Chunk{
			{ID: "venue@v1#c1", SectionID: secA.ID, Text: secA.Text},
			{ID: "signup@v1#c1", SectionID: secB.ID, Text: secB.Text},
		},
	)
	e := engine.New(r)

	// 无子问题计划：只答场租，报名费文档不应被引出
	base := e.AnswerWithScope("活动场地租用一天要多少钱", nil, "")
	if base.Status != "answered" {
		t.Fatalf("基线回答状态 = %s: %s", base.Status, base.Answer)
	}
	if strings.Contains(base.Answer, "一百") {
		t.Errorf("无子问题计划不应包含报名费: %s", base.Answer)
	}

	// 带子问题计划：报名费候选被补充进证据池
	ans := e.AnswerWithScopeSub("活动场地租用一天要多少钱", nil, "", []string{"报名费是多少"})
	if ans.Status != "answered" {
		t.Fatalf("扩充后状态 = %s: %s", ans.Status, ans.Answer)
	}
	if !strings.Contains(ans.Answer, "三千") || !strings.Contains(ans.Answer, "一百") {
		t.Errorf("子问题扩充应同时覆盖场租与报名费: %s", ans.Answer)
	}
	foundB := false
	for _, c := range ans.Citations {
		if c.DocumentID == "signup" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("signup 文档应出现在引用中: %+v", ans.Citations)
	}
}

// TestAnswerWithScopeSubKeepsDeterminism 子问题不改变既有单查询行为（同输入同输出）。
func TestAnswerWithScopeSubKeepsDeterminism(t *testing.T) {
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sec := &types.Section{ID: "fee@v1#s1", DocID: "fee", Version: "v1", SectionPath: "费用",
		Text: "申请费用为一百元。"}
	r := retriever.New()
	r.Index([]*types.Document{{ID: "fee", Version: "v1", Title: "申请费用", EffectiveDate: date}},
		[]*types.Section{sec}, []*types.Chunk{{ID: "fee@v1#c1", SectionID: sec.ID, Text: sec.Text}})
	e := engine.New(r)
	a := e.AnswerWithScope("申请费用是多少", nil, "")
	b := e.AnswerWithScopeSub("申请费用是多少", nil, "", []string{"申请费用是多少"})
	if a.Answer != b.Answer || len(a.Citations) != len(b.Citations) {
		t.Errorf("与原问题相同的子问题不应改变结果:\nA=%s\nB=%s", a.Answer, b.Answer)
	}
}

// TestModalNegationFamily 否定判定族回退词表：是非问句的证据含"不开放"等否定表述时应放行
// （UI 实测"咨询窗口法定节假日开放吗"因词表缺"不开放"被误拒）。
func TestModalNegationFamily(t *testing.T) {
	date := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	sec := &types.Section{ID: "consult@v1#s1", DocID: "consult", Version: "v1", SectionPath: "服务时间",
		Text: "现场咨询窗口设于社区服务中心一楼大厅，工作日九时至十七时开放，法定节假日不对外开放。"}
	r := retriever.New()
	r.Index([]*types.Document{{ID: "consult", Version: "v1", Title: "咨询窗口安排", EffectiveDate: date}},
		[]*types.Section{sec}, []*types.Chunk{{ID: "consult@v1#c1", SectionID: sec.ID, Text: sec.Text}})
	e := engine.New(r)
	ans := e.AnswerWithScope("咨询窗口法定节假日开放吗", nil, "")
	if ans.Status != "answered" {
		t.Errorf("含'不对外开放'的是非问应回答，实际 %s: %s", ans.Status, ans.Answer)
	}
}
