package store

import "seedvault/internal/domain"

func cloneTrial(value domain.GerminationTrial) domain.GerminationTrial {
	value.ObservationSchedule = append([]string(nil), value.ObservationSchedule...)
	value.Observations = append([]domain.Observation(nil), value.Observations...)
	value.Issues = append([]domain.Issue(nil), value.Issues...)
	value.EvidenceNotes = append([]domain.EvidenceNote(nil), value.EvidenceNotes...)
	value.RemediationItems = append([]domain.RemediationItem(nil), value.RemediationItems...)
	value.Review = cloneReview(value.Review)
	value.ReviewHistory = append([]domain.Review(nil), value.ReviewHistory...)
	for i := range value.ReviewHistory {
		value.ReviewHistory[i] = cloneReview(value.ReviewHistory[i])
	}
	value.DesignSummary.PlannedStages = append([]string(nil), value.DesignSummary.PlannedStages...)
	return value
}

func cloneReview(value domain.Review) domain.Review {
	value.RemediationItems = append([]domain.RemediationItem(nil), value.RemediationItems...)
	return value
}
