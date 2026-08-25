package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ReplicateSummary struct {
	ReplicateNo, ObservationCount, TotalGerminated, TotalMold int
	LatestObservedAt                                          string
	AverageTemperature, AverageHumidity                       float64
}
type StageSummary struct {
	ObservedAt                                            string
	Replicates, TotalGerminated, TotalMold, EvidenceCount int
	AverageTemperature, AverageHumidity                   float64
}
type Readiness struct {
	CanReview, CanFreeze       bool
	MissingStages              []string
	MissingGroups              []int
	OpenIssues                 []string
	IncompleteRemediationItems []string
	Reasons                    []string
}

type MissingObservation struct {
	Stage       string
	ReplicateNo int
}
type ArchiveIntegrityChecklist struct {
	Valid                                           bool
	MissingObservations                             []MissingObservation
	OpenIssues, IncompleteRemediationItems, Reasons []string
	LatestReviewPassed                              bool
}

type IssueGroup struct {
	ObservationID string
	Observation   Observation
	Issues        []Issue
	PendingCount  int
}

func (t GerminationTrial) ReplicateSummaries() []ReplicateSummary {
	items := map[int]*ReplicateSummary{}
	for _, o := range t.Observations {
		item := items[o.ReplicateNo]
		if item == nil {
			item = &ReplicateSummary{ReplicateNo: o.ReplicateNo}
			items[o.ReplicateNo] = item
		}
		item.ObservationCount++
		item.TotalGerminated += o.GerminatedCount
		item.TotalMold += o.MoldCount
		item.AverageTemperature += o.TemperatureCelsius
		item.AverageHumidity += o.HumidityPercent
		if item.LatestObservedAt == "" || o.ObservedAt > item.LatestObservedAt {
			item.LatestObservedAt = o.ObservedAt
		}
	}
	out := make([]ReplicateSummary, 0, len(items))
	for _, v := range items {
		v.AverageTemperature /= float64(v.ObservationCount)
		v.AverageHumidity /= float64(v.ObservationCount)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReplicateNo < out[j].ReplicateNo })
	return out
}
func (t GerminationTrial) StageSummaries() []StageSummary {
	items := map[string]*StageSummary{}
	for _, o := range t.Observations {
		item := items[o.ObservedAt]
		if item == nil {
			item = &StageSummary{ObservedAt: o.ObservedAt}
			items[o.ObservedAt] = item
		}
		item.Replicates++
		item.TotalGerminated += o.GerminatedCount
		item.TotalMold += o.MoldCount
		item.AverageTemperature += o.TemperatureCelsius
		item.AverageHumidity += o.HumidityPercent
		if o.EvidenceRef != "" {
			item.EvidenceCount++
		}
	}
	out := make([]StageSummary, 0, len(items))
	for _, v := range items {
		v.AverageTemperature /= float64(v.Replicates)
		v.AverageHumidity /= float64(v.Replicates)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		return scheduleIndex(t.ObservationSchedule, out[i].ObservedAt) < scheduleIndex(t.ObservationSchedule, out[j].ObservedAt)
	})
	return out
}
func scheduleIndex(schedule []string, stage string) int {
	for i, v := range schedule {
		if v == stage {
			return i
		}
	}
	return len(schedule) + 1
}
func (t GerminationTrial) Readiness() Readiness {
	r := Readiness{}
	stages := map[string]bool{}
	groups := map[int]bool{}
	for _, o := range t.Observations {
		stages[o.ObservedAt] = true
		groups[o.ReplicateNo] = true
	}
	for _, stage := range t.ObservationSchedule {
		if !stages[stage] {
			r.MissingStages = append(r.MissingStages, stage)
		}
	}
	for i := 1; i <= t.ReplicateCount; i++ {
		if !groups[i] {
			r.MissingGroups = append(r.MissingGroups, i)
		}
	}
	for _, issue := range t.Issues {
		if issue.Status == "open" {
			r.OpenIssues = append(r.OpenIssues, issue.ID)
		}
	}
	for _, item := range t.RemediationItems {
		if item.Status != "completed" {
			r.IncompleteRemediationItems = append(r.IncompleteRemediationItems, item.ID)
		}
	}
	if len(t.Observations) == 0 {
		r.Reasons = append(r.Reasons, "尚无观测记录")
	}
	if len(r.OpenIssues) > 0 {
		r.Reasons = append(r.Reasons, "存在未裁决异常")
	}
	if len(r.IncompleteRemediationItems) > 0 {
		r.Reasons = append(r.Reasons, "存在未完成整改项")
	}
	if t.FrozenAt != "" {
		r.Reasons = append(r.Reasons, "试验已经封存")
	}
	r.CanReview = len(t.Observations) > 0 && len(r.OpenIssues) == 0 && len(r.IncompleteRemediationItems) == 0 && t.FrozenAt == ""
	r.CanFreeze = r.CanReview && t.ReviewState == "approved"
	return r
}

func (t GerminationTrial) ArchiveIntegrity() ArchiveIntegrityChecklist {
	c := ArchiveIntegrityChecklist{LatestReviewPassed: t.ReviewState == "approved" || t.ReviewState == "frozen"}
	observed := map[string]bool{}
	for _, o := range t.Observations {
		observed[fmt.Sprintf("%s\x00%d", o.ObservedAt, o.ReplicateNo)] = true
	}
	for _, stage := range t.ObservationSchedule {
		for group := 1; group <= t.ReplicateCount; group++ {
			if !observed[fmt.Sprintf("%s\x00%d", stage, group)] {
				c.MissingObservations = append(c.MissingObservations, MissingObservation{Stage: stage, ReplicateNo: group})
			}
		}
	}
	for _, issue := range t.Issues {
		if issue.Status == "open" {
			c.OpenIssues = append(c.OpenIssues, issue.ID)
		}
	}
	for _, item := range t.RemediationItems {
		if item.Status != "completed" {
			c.IncompleteRemediationItems = append(c.IncompleteRemediationItems, item.ID)
		}
	}
	if len(c.MissingObservations) > 0 {
		coords := make([]string, 0, len(c.MissingObservations))
		for _, p := range c.MissingObservations {
			coords = append(coords, fmt.Sprintf("%s/重复组%d", p.Stage, p.ReplicateNo))
		}
		c.Reasons = append(c.Reasons, "缺少计划观测："+strings.Join(coords, "、"))
	}
	if len(c.OpenIssues) > 0 {
		c.Reasons = append(c.Reasons, "未裁决异常："+strings.Join(c.OpenIssues, "、"))
	}
	if len(c.IncompleteRemediationItems) > 0 {
		c.Reasons = append(c.Reasons, "未完成整改项："+strings.Join(c.IncompleteRemediationItems, "、"))
	}
	if !c.LatestReviewPassed {
		c.Reasons = append(c.Reasons, "最新复核尚未通过")
	}
	c.Valid = len(c.Reasons) == 0
	return c
}

func (t GerminationTrial) IssueGroups() []IssueGroup {
	groups := map[string]*IssueGroup{}
	order := []string{}
	for _, issue := range t.Issues {
		g := groups[issue.ObservationID]
		if g == nil {
			g = &IssueGroup{ObservationID: issue.ObservationID}
			for _, o := range t.Observations {
				if o.ID == issue.ObservationID {
					g.Observation = o
					break
				}
			}
			groups[issue.ObservationID], order = g, append(order, issue.ObservationID)
		}
		g.Issues = append(g.Issues, issue)
		if issue.Status == "open" {
			g.PendingCount++
		}
	}
	out := make([]IssueGroup, 0, len(order))
	for _, id := range order {
		out = append(out, *groups[id])
	}
	return out
}

func (t GerminationTrial) OpenIssueCount() int {
	n := 0
	for _, issue := range t.Issues {
		if issue.Status == "open" {
			n++
		}
	}
	return n
}

func (t GerminationTrial) LastReturnedReview() Review {
	for i := len(t.ReviewHistory) - 1; i >= 0; i-- {
		if t.ReviewHistory[i].State == "returned" {
			return t.ReviewHistory[i]
		}
	}
	return Review{}
}
func (t GerminationTrial) NextObservationStage() string {
	counts := map[string]int{}
	for _, o := range t.Observations {
		counts[o.ObservedAt]++
	}
	for _, stage := range t.ObservationSchedule {
		if counts[stage] < t.ReplicateCount {
			return stage
		}
	}
	return ""
}
func (t GerminationTrial) ValidateSnapshot() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.SeedSourceID) == "" {
		return fmt.Errorf("%w：试验标识或种源引用缺失", ErrInvalid)
	}
	if t.Version < 1 {
		return fmt.Errorf("%w：版本号无效", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, o := range t.Observations {
		if o.TrialID != t.ID {
			return fmt.Errorf("%w：观测引用了其他试验", ErrInvalid)
		}
		if seen[o.ID] {
			return fmt.Errorf("%w：观测标识重复", ErrInvalid)
		}
		seen[o.ID] = true
	}
	if t.FrozenAt != "" {
		if _, e := time.Parse(time.RFC3339, t.FrozenAt); e != nil {
			return fmt.Errorf("%w：冻结时间无效", ErrInvalid)
		}
		if t.ReviewState != "frozen" {
			return fmt.Errorf("%w：冻结状态不一致", ErrInvalid)
		}
	}
	return nil
}
