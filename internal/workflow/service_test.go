package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"seedvault/internal/domain"
	"seedvault/internal/store"
	"strings"
	"testing"
	"time"
)

func TestEndToEnd(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	w := New(s)
	src, e := w.CreateSource(SourceInput{ScientificName: "学名", CollectionLocation: "地点", Collector: "人", CollectionDate: "2026-01-01", StorageCondition: "4C", LotCode: "LOT"})
	if e != nil {
		t.Fatal(e)
	}
	tr, e := w.CreateTrial(TrialInput{SourceID: src.ID, ProtocolName: "协议", ReplicateCount: 1, TemperatureCelsius: 22, ObservationSchedule: []string{"第1天"}})
	if e != nil {
		t.Fatal(e)
	}
	tr, issue, _, e := w.AddObservation(tr.ID, ObservationInput{ReplicateNo: 1, ObservedAt: "第1天", GerminatedCount: 2, MoldCount: 0, TemperatureCelsius: 22, HumidityPercent: 60, EvidenceRef: "photo-1"}, tr.Version)
	if e != nil {
		t.Fatal(e)
	}
	if issue.ID != "" {
		t.Fatal("不应生成异常")
	}
	tr, e = w.Review(tr.ID, "复核员", "approved", "通过", "", tr.Version)
	if e != nil {
		t.Fatal(e)
	}
	_, c, e := w.Freeze(tr.ID, "签发人", tr.Version)
	if e != nil || c.SnapshotHash == "" {
		t.Fatalf("封存失败: %v", e)
	}
	ok, _, e := w.Verify(c.ID)
	if e != nil || !ok {
		t.Fatalf("验证失败: %v", e)
	}
}

func TestSourceConflictStatusAndDesignPreflight(t *testing.T) {
	s, _ := store.Open("")
	w := New(s)
	src, err := w.CreateSource(SourceInput{ScientificName: "学名", CollectionLocation: "地点", Collector: "人", CollectionDate: "2026-01-01", StorageCondition: "4C", LotCode: " LOT-001 "})
	if err != nil {
		t.Fatal(err)
	}
	events := s.EventCount()
	_, err = w.CreateSource(SourceInput{ScientificName: "学名2", CollectionLocation: "地点", Collector: "人", CollectionDate: "2026-01-01", StorageCondition: "4C", LotCode: "lot-001"})
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), src.ID) || s.EventCount() != events {
		t.Fatalf("批次冲突未保持原子性: %v", err)
	}
	src, err = w.UpdateSourceStatus(src.ID, "停用", src.Version)
	if err != nil {
		t.Fatal(err)
	}
	events = s.EventCount()
	_, err = w.CreateTrial(TrialInput{SourceID: src.ID, ProtocolName: "协议", ReplicateCount: 3, TemperatureCelsius: 22, ObservationSchedule: []string{"第3天", "第7天"}})
	if err == nil || !strings.Contains(err.Error(), "停用") || s.EventCount() != events {
		t.Fatalf("停用拦截失败: %v", err)
	}
	src, err = w.UpdateSourceStatus(src.ID, "active", src.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := w.CreateTrial(TrialInput{SourceID: src.ID, ProtocolName: "协议", ReplicateCount: 3, TemperatureCelsius: 22, ObservationSchedule: []string{"3天", "第7天"}})
	if err != nil {
		t.Fatal(err)
	}
	if trial.DesignSummary.EstimatedRecords != 6 || trial.ObservationSchedule[0] != "第3天" {
		t.Fatalf("设计摘要错误: %+v", trial.DesignSummary)
	}
	_, err = w.CreateTrial(TrialInput{SourceID: src.ID, ProtocolName: "协议", ReplicateCount: 3, TemperatureCelsius: 22, ObservationSchedule: []string{"第3天", "7天"}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("活动设计冲突未识别: %v", err)
	}
	view, _ := s.Source(src.ID)
	if view.AssociatedTrialCount != 1 || view.UnarchivedTrialCount != 1 || !view.CanCreateTrial {
		t.Fatalf("种源占用投影错误: %+v", view)
	}
}

func TestBatchObservationAndMergedDecisionAreAtomic(t *testing.T) {
	w, s, trial := newTestTrial(t, 3, []string{"第3天", "第7天"})
	events := s.EventCount()
	_, _, err := w.AddObservations(trial.ID, []ObservationInput{{ReplicateNo: 1, ObservedAt: "第3天"}, {ReplicateNo: 4, ObservedAt: "第3天"}}, trial.Version)
	if err == nil || s.EventCount() != events {
		t.Fatalf("越界批量观测应全部拒绝: %v", err)
	}
	rows := []ObservationInput{}
	for i := 1; i <= 3; i++ {
		rows = append(rows, ObservationInput{ReplicateNo: i, ObservedAt: "3天", GerminatedCount: 2, MoldCount: 4, TemperatureCelsius: 22, HumidityPercent: 95})
	}
	trial, issues, err := w.AddObservations(trial.ID, rows, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	if trial.Version != 2 || len(trial.Observations) != 3 || len(issues) != 9 {
		t.Fatalf("批量结果错误: v%d obs=%d issues=%d", trial.Version, len(trial.Observations), len(issues))
	}
	group := trial.IssueGroups()[0]
	bad := make([]DecisionInput, len(group.Issues))
	for i, issue := range group.Issues {
		bad[i] = DecisionInput{IssueID: issue.ID, Reason: "环境偏差", Disposition: "隔离"}
	}
	_, err = w.DecideIssues(trial.ID, group.Issues[0].ID, bad, trial.Version)
	if err == nil {
		t.Fatal("缺证异常没有证据时应整组拒绝")
	}
	after, _ := s.Trial(trial.ID)
	if after.Version != trial.Version || after.OpenIssueCount() != 9 {
		t.Fatal("失败裁决改变了聚合")
	}
	for i := range bad {
		bad[i].Evidence = "photo:decision"
	}
	after, err = w.DecideIssues(trial.ID, group.Issues[0].ID, bad, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	batch := after.Issues[0].DecisionBatchID
	if batch == "" {
		t.Fatal("未生成统一处置批次号")
	}
	for _, issue := range after.Issues[:3] {
		if issue.Status != "decided" || issue.DecisionBatchID != batch {
			t.Fatalf("合并裁决结果不一致: %+v", issue)
		}
	}
}

func TestRemediationFreezeChecklistAndReceipt(t *testing.T) {
	w, s, trial := newTestTrial(t, 2, []string{"第3天", "第7天"})
	rows := func(stage string) []ObservationInput {
		return []ObservationInput{{ReplicateNo: 1, ObservedAt: stage, GerminatedCount: 2, TemperatureCelsius: 22, HumidityPercent: 60, EvidenceRef: stage + "-1"}, {ReplicateNo: 2, ObservedAt: stage, GerminatedCount: 3, TemperatureCelsius: 22, HumidityPercent: 60, EvidenceRef: stage + "-2"}}
	}
	var err error
	trial, _, err = w.AddObservations(trial.ID, rows("第3天"), trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, _, err = w.AddObservations(trial.ID, rows("第7天"), trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, err = w.ReviewWithItems(trial.ID, ReviewInput{Reviewer: "复核员", State: "returned", Comment: "需整改", RemediationItems: []RemediationItemInput{{Problem: "观测证据", ResponsibleRole: "实验员", CompletionCriteria: "补图"}, {Problem: "处置说明", ResponsibleRole: "实验员", CompletionCriteria: "补文档"}}}, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	items := trial.RemediationItems
	trial, err = w.AddEvidence(trial.ID, EvidenceInput{Author: "实验员", Kind: "remediation", Reference: "fix-1", Comment: "完成", RemediationItemID: items[0].ID}, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	events := s.EventCount()
	_, err = w.Review(trial.ID, "复核员", "approved", "通过", "", trial.Version)
	if err == nil || !strings.Contains(err.Error(), items[1].ID) || s.EventCount() != events {
		t.Fatalf("未完成整改未阻断送审: %v", err)
	}
	trial, err = w.AddEvidence(trial.ID, EvidenceInput{Author: "实验员", Kind: "remediation", Reference: "fix-2", Comment: "完成", RemediationItemID: items[1].ID}, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, err = w.Review(trial.ID, "复核员", "approved", "通过", "", trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, cert, err := w.Freeze(trial.ID, "签发员", trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := w.VerifyReceipt(cert.ID)
	if err != nil || !receipt.Valid || !receipt.SnapshotHashValid || !receipt.AuditChainValid || !receipt.CredentialStatusValid || !receipt.FrozenVersionValid {
		t.Fatalf("验证回执无效: %+v %v", receipt, err)
	}
	count := len(s.Certificates())
	_, again, err := w.Freeze(trial.ID, "签发员", trial.Version)
	if err != nil || again.ID != cert.ID || len(s.Certificates()) != count {
		t.Fatalf("重复冻结未返回原凭据: %+v %v", again, err)
	}
}

func TestFreezeReportsMissingCoordinate(t *testing.T) {
	w, s, trial := newTestTrial(t, 2, []string{"第7天"})
	trial, _, _, err := w.AddObservation(trial.ID, ObservationInput{ReplicateNo: 1, ObservedAt: "第7天", GerminatedCount: 1, TemperatureCelsius: 22, HumidityPercent: 60, EvidenceRef: "photo"}, trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	trial, err = w.Review(trial.ID, "复核员", "approved", "通过", "", trial.Version)
	if err != nil {
		t.Fatal(err)
	}
	events := s.EventCount()
	_, _, err = w.Freeze(trial.ID, "签发员", trial.Version)
	if err == nil || !strings.Contains(err.Error(), "第7天/重复组2") || s.EventCount() != events || len(s.Certificates()) != 0 {
		t.Fatalf("缺失坐标封存检查错误: %v", err)
	}
	after, _ := s.Trial(trial.ID)
	if after.ReviewState != "approved" {
		t.Fatal("失败封存改变了状态")
	}
}

func newTestTrial(t *testing.T, replicas int, schedule []string) (*Service, *store.Store, domain.GerminationTrial) {
	t.Helper()
	s, _ := store.Open("")
	w := New(s)
	src, err := w.CreateSource(SourceInput{ScientificName: "学名", CollectionLocation: "地点", Collector: "人", CollectionDate: "2026-01-01", StorageCondition: "4C", LotCode: fmt.Sprintf("LOT-%d-%d", replicas, time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := w.CreateTrial(TrialInput{SourceID: src.ID, ProtocolName: "协议", ReplicateCount: replicas, TemperatureCelsius: 22, ObservationSchedule: schedule})
	if err != nil {
		t.Fatal(err)
	}
	return w, s, trial
}
