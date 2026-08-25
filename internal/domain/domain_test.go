package domain

import "testing"

func TestTrialLifecycleAndThreshold(t *testing.T) {
	tr, err := NewTrial("t1", "s1", "协议", 2, 22, []string{"第3天"})
	if err != nil {
		t.Fatal(err)
	}
	i, created, err := tr.AddObservation(Observation{ID: "o1", TrialID: "t1", ReplicateNo: 1, ObservedAt: "第3天", GerminatedCount: 4, MoldCount: 3, TemperatureCelsius: 22, HumidityPercent: 70, EvidenceRef: "photo-1"})
	if err != nil || !created || i.Status != "open" {
		t.Fatalf("异常未生成: %+v %v", i, err)
	}
	if err := tr.SubmitReview("复核员", "approved", "先裁决", ""); err == nil {
		t.Fatal("未裁决异常不应通过复核")
	}
	if err := tr.DecideIssue(i.ID, "污染", "隔离", "photo-1"); err != nil {
		t.Fatal(err)
	}
	if err := tr.SubmitReview("复核员", "approved", "证据齐全", ""); err != nil {
		t.Fatal(err)
	}
	if err := tr.Freeze(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Freeze(); err != ErrFrozen {
		t.Fatalf("重复冻结错误: %v", err)
	}
}

func TestValidation(t *testing.T) {
	if _, err := NewSeedSource("s", "", "地点", "人", "2026-01-01", "4C", "L"); err == nil {
		t.Fatal("应拒绝空学名")
	}
	if _, err := NewTrial("t", "s", "p", 0, 22, []string{"第1天"}); err == nil {
		t.Fatal("应拒绝零重复组")
	}
}
