package store

import "seedvault/internal/domain"

func cloneTrial(value domain.GerminationTrial) domain.GerminationTrial {
	value.ObservationSchedule = append([]string(nil), value.ObservationSchedule...)
	value.Observations = append([]domain.Observation(nil), value.Observations...)
	value.Issues = append([]domain.Issue(nil), value.Issues...)
	return value
}
