package audit

import (
	"sort"

	"seedvault/internal/domain"
)

func canonicalSnapshot(trial domain.GerminationTrial) domain.GerminationTrial {
	clone := trial
	clone.Observations = append([]domain.Observation(nil), trial.Observations...)
	clone.Issues = append([]domain.Issue(nil), trial.Issues...)
	sort.Slice(clone.Observations, func(i, j int) bool {
		return clone.Observations[i].ID < clone.Observations[j].ID
	})
	sort.Slice(clone.Issues, func(i, j int) bool {
		return clone.Issues[i].ID < clone.Issues[j].ID
	})
	return clone
}
