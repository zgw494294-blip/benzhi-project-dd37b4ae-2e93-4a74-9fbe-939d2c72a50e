package store

import (
	"sort"
	"strings"

	"seedvault/internal/domain"
)

type TrialFilter struct {
	SeedSourceID, ReviewState, ProtocolContains string
	HasOpenIssues, OnlyFrozen                   bool
	Offset, Limit                               int
}
type SourceFilter struct {
	ScientificName, Location, LotCode, Status string
	Offset, Limit                             int
}
type TrialPage struct {
	Items                []domain.GerminationTrial
	Total, Offset, Limit int
}
type SourcePage struct {
	Items                []domain.SeedSource
	Total, Offset, Limit int
}

func (s *Store) QueryTrials(filter TrialFilter) TrialPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.trialQueries[filter]; ok {
		return cloneTrialPage(cached)
	}
	items := make([]domain.GerminationTrial, 0, len(s.trials))
	for _, trial := range s.trials {
		if filter.SeedSourceID != "" && trial.SeedSourceID != filter.SeedSourceID {
			continue
		}
		if filter.ReviewState != "" && trial.ReviewState != filter.ReviewState {
			continue
		}
		if filter.ProtocolContains != "" && !strings.Contains(strings.ToLower(trial.ProtocolName), strings.ToLower(filter.ProtocolContains)) {
			continue
		}
		if filter.OnlyFrozen && trial.FrozenAt == "" {
			continue
		}
		if filter.HasOpenIssues && !hasOpenIssue(trial) {
			continue
		}
		items = append(items, cloneTrial(trial))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ReviewState == items[j].ReviewState {
			return items[i].ID < items[j].ID
		}
		return stateOrder(items[i].ReviewState) < stateOrder(items[j].ReviewState)
	})
	total := len(items)
	offset, limit := normalizePage(filter.Offset, filter.Limit, total)
	end := offset + limit
	if end > total {
		end = total
	}
	page := TrialPage{Items: append([]domain.GerminationTrial(nil), items[offset:end]...), Total: total, Offset: offset, Limit: limit}
	s.trialQueries[filter] = cloneTrialPage(page)
	return cloneTrialPage(page)
}

func cloneTrialPage(page TrialPage) TrialPage {
	result := page
	result.Items = make([]domain.GerminationTrial, len(page.Items))
	for index := range page.Items {
		result.Items[index] = cloneTrial(page.Items[index])
	}
	return result
}
func (s *Store) QuerySources(filter SourceFilter) SourcePage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.SeedSource, 0, len(s.sources))
	for _, source := range s.sources {
		if filter.ScientificName != "" && !strings.Contains(strings.ToLower(source.ScientificName), strings.ToLower(filter.ScientificName)) {
			continue
		}
		if filter.Location != "" && !strings.Contains(source.CollectionLocation, filter.Location) {
			continue
		}
		if filter.LotCode != "" && domain.NormalizeLotCode(source.LotCode) != domain.NormalizeLotCode(filter.LotCode) {
			continue
		}
		if filter.Status != "" && source.Status != filter.Status {
			continue
		}
		items = append(items, s.decorateSourceLocked(source))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ScientificName == items[j].ScientificName {
			return items[i].LotCode < items[j].LotCode
		}
		return items[i].ScientificName < items[j].ScientificName
	})
	total := len(items)
	offset, limit := normalizePage(filter.Offset, filter.Limit, total)
	end := offset + limit
	if end > total {
		end = total
	}
	return SourcePage{Items: append([]domain.SeedSource(nil), items[offset:end]...), Total: total, Offset: offset, Limit: limit}
}
func (s *Store) Certificates() []domain.ArchiveCertificate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.ArchiveCertificate, 0, len(s.certificates))
	for _, v := range s.certificates {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].IssuedAt > items[j].IssuedAt })
	return items
}
func hasOpenIssue(trial domain.GerminationTrial) bool {
	for _, issue := range trial.Issues {
		if issue.Status == "open" {
			return true
		}
	}
	return false
}
func stateOrder(state string) int {
	switch state {
	case "returned":
		return 0
	case "draft":
		return 1
	case "approved":
		return 2
	case "frozen":
		return 3
	default:
		return 4
	}
}
