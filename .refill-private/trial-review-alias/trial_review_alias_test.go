package trial_review_alias_test

import (
	"testing"

	"seedvault/internal/domain"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

func TestTrialSnapshotDoesNotAliasReviewState(t *testing.T) {
	db, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	trial := domain.GerminationTrial{
		ID:                  "trial-alias",
		SeedSourceID:        "source-alias",
		ObservationSchedule: []string{"第3天"},
		DesignSummary:       domain.DesignSummary{PlannedStages: []string{"第3天"}},
		EvidenceNotes:       []domain.EvidenceNote{{ID: "evidence-1", Reference: "photo:original"}},
		RemediationItems:    []domain.RemediationItem{{ID: "remediation-1", Status: "open"}},
		Review: domain.Review{
			Comment:          "原始复核意见",
			RemediationItems: []domain.RemediationItem{{ID: "remediation-1", Status: "open"}},
		},
		ReviewHistory: []domain.Review{{
			Comment:          "原始历史意见",
			RemediationItems: []domain.RemediationItem{{ID: "remediation-1", Status: "open"}},
		}},
	}
	if err := db.PutTrial(trial, 0); err != nil {
		t.Fatal(err)
	}

	service := workflow.New(db)
	snapshot, ok := service.GetTrial(trial.ID)
	if !ok {
		t.Fatal("试验快照不存在")
	}
	snapshot.DesignSummary.PlannedStages[0] = "第99天"
	snapshot.EvidenceNotes[0].Reference = "photo:tampered"
	snapshot.RemediationItems[0].Status = "completed"
	snapshot.Review.RemediationItems[0].Status = "completed"
	snapshot.ReviewHistory[0].Comment = "被调用方篡改"
	snapshot.ReviewHistory[0].RemediationItems[0].Status = "completed"

	fresh, ok := service.GetTrial(trial.ID)
	if !ok {
		t.Fatal("试验快照在二次读取时不存在")
	}
	if fresh.DesignSummary.PlannedStages[0] != "第3天" ||
		fresh.EvidenceNotes[0].Reference != "photo:original" ||
		fresh.RemediationItems[0].Status != "open" ||
		fresh.Review.RemediationItems[0].Status != "open" ||
		fresh.ReviewHistory[0].Comment != "原始历史意见" ||
		fresh.ReviewHistory[0].RemediationItems[0].Status != "open" {
		t.Fatalf("读取快照污染了存储中的复核状态: %+v", fresh)
	}
}
