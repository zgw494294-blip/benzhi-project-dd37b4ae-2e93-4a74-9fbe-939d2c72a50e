package store

import "seedvault/internal/domain"

func cloneTrial(value domain.GerminationTrial) domain.GerminationTrial {
	value.ObservationSchedule = append([]string(nil), value.ObservationSchedule...)
	value.Observations = append([]domain.Observation(nil), value.Observations...)
	value.Issues = append([]domain.Issue(nil), value.Issues...)
	value.ReviewHistory = append([]domain.Review(nil), value.ReviewHistory...)
	value.EvidenceNotes = append([]domain.EvidenceNote(nil), value.EvidenceNotes...)
	value.RemediationItems = append([]domain.RemediationItem(nil), value.RemediationItems...)
	value.DesignSummary.PlannedStages = append([]string(nil), value.DesignSummary.PlannedStages...)
	value.Review.RemediationItems = append([]domain.RemediationItem(nil), value.Review.RemediationItems...)
	for index := range value.ReviewHistory {
		value.ReviewHistory[index].RemediationItems = append([]domain.RemediationItem(nil), value.ReviewHistory[index].RemediationItems...)
	}
	return value
}
