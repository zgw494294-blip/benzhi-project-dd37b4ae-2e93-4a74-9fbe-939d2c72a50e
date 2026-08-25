package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("领域校验失败")
	ErrConflict = errors.New("版本冲突")
	ErrNotFound = errors.New("记录不存在")
	ErrFrozen   = errors.New("试验已封存")
)

type SeedSource struct {
	ID, ScientificName, CollectionLocation, Collector, CollectionDate, StorageCondition, LotCode, Status string
	Version                                                                                              int `json:"version"`
	AssociatedTrialCount, UnarchivedTrialCount                                                           int
	CanCreateTrial                                                                                       bool
}

type Observation struct {
	ID, TrialID, ObservedAt, EvidenceRef    string
	ReplicateNo, GerminatedCount, MoldCount int
	TemperatureCelsius, HumidityPercent     float64
}

type Issue struct {
	ID, ObservationID, Kind, Message, Status, Reason, Disposition, EvidenceRef string
	CreatedAt, DecidedAt, DecisionBatchID                                      string
}

type Review struct {
	State, Reviewer, Comment, Remediation string
	ReviewedAt                            string
	RemediationItems                      []RemediationItem
	ChangeSummary                         string
}

type EvidenceNote struct {
	ID, Author, Kind, Reference, Comment, CreatedAt, RemediationItemID string
}

type RemediationItem struct {
	ID, Problem, ResponsibleRole, CompletionCriteria string
	Status, EvidenceRef, CompletionNote, CompletedAt string
	CompletedVersion                                 int
}

type DesignSummary struct {
	PlannedStages      []string
	ReplicatesPerStage int
	EstimatedRecords   int
	NextObservation    string
}

type GerminationTrial struct {
	ID, SeedSourceID, ProtocolName, ReviewState string
	ReplicateCount                              int
	TemperatureCelsius                          float64
	ObservationSchedule                         []string
	Observations                                []Observation
	Issues                                      []Issue
	Review                                      Review
	ReviewHistory                               []Review
	EvidenceNotes                               []EvidenceNote
	RemediationItems                            []RemediationItem
	DesignSummary                               DesignSummary
	Version                                     int `json:"version"`
	FrozenAt                                    string
}

type ArchiveCertificate struct {
	ID, TrialID, SnapshotHash, IssuedAt, Issuer, VerificationNonce, Status string
	AuditChainHead                                                         string
	FrozenVersion                                                          int
}

type AuditEntry struct {
	Sequence        int
	ID              string
	AggregateID     string
	Action          string
	Actor           string
	Role            string
	RequestKey      string
	OccurredAt      string
	ExpectedVersion int
	ResultVersion   int
	Details         map[string]string
	PreviousHash    string
	Hash            string
}

func NewSeedSource(id, scientificName, location, collector, collectionDate, condition, lot string) (SeedSource, error) {
	if err := validateSourceIdentity(scientificName, location, collector, collectionDate, condition, lot); err != nil {
		return SeedSource{}, err
	}
	if _, err := time.Parse("2006-01-02", collectionDate); err != nil {
		return SeedSource{}, fmt.Errorf("%w：采集日期格式应为YYYY-MM-DD", ErrInvalid)
	}
	v := SeedSource{ID: id, ScientificName: strings.TrimSpace(scientificName), CollectionLocation: strings.TrimSpace(location), Collector: strings.TrimSpace(collector), CollectionDate: collectionDate, StorageCondition: strings.TrimSpace(condition), LotCode: NormalizeLotCode(lot), Status: "active", Version: 1, CanCreateTrial: true}
	if err := ValidateSource(v, time.Now()); err != nil {
		return SeedSource{}, err
	}
	return v, nil
}

func NewTrial(id, sourceID, protocol string, replicas int, temperature float64, schedule []string) (GerminationTrial, error) {
	sourceID, protocol = strings.TrimSpace(sourceID), strings.TrimSpace(protocol)
	if sourceID == "" || protocol == "" {
		return GerminationTrial{}, fmt.Errorf("%w：种源和协议不能为空", ErrInvalid)
	}
	if replicas < 1 || replicas > 96 {
		return GerminationTrial{}, fmt.Errorf("%w：重复组数量应为1至96", ErrInvalid)
	}
	if temperature < -20 || temperature > 60 {
		return GerminationTrial{}, fmt.Errorf("%w：温度范围无效", ErrInvalid)
	}
	if len(schedule) == 0 {
		return GerminationTrial{}, fmt.Errorf("%w：至少配置一个观测时间点", ErrInvalid)
	}
	normalized, summary, err := PreflightTrialDesign(replicas, schedule)
	if err != nil {
		return GerminationTrial{}, err
	}
	v := GerminationTrial{ID: id, SeedSourceID: sourceID, ProtocolName: strings.TrimSpace(protocol), ReplicateCount: replicas, TemperatureCelsius: temperature, ObservationSchedule: normalized, DesignSummary: summary, ReviewState: "draft", Version: 1}
	if err := ValidateTrialDesign(v); err != nil {
		return GerminationTrial{}, err
	}
	return v, nil
}

func (t *GerminationTrial) AddObservation(o Observation) (Issue, bool, error) {
	issues, err := t.AddObservations([]Observation{o})
	if err != nil || len(issues) == 0 {
		return Issue{}, false, err
	}
	return issues[0], true, nil
}

func (t *GerminationTrial) AddObservations(observations []Observation) ([]Issue, error) {
	if t.FrozenAt != "" {
		return nil, ErrFrozen
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("%w：批量观测不能为空", ErrInvalid)
	}
	stage := strings.TrimSpace(observations[0].ObservedAt)
	if !contains(t.ObservationSchedule, stage) {
		return nil, fmt.Errorf("%w：观测阶段不在设计时间表中：%s", ErrInvalid, stage)
	}
	groups := map[int]bool{}
	observationIDs := map[string]bool{}
	for _, old := range t.Observations {
		observationIDs[old.ID] = true
	}
	for row, o := range observations {
		if strings.TrimSpace(o.ID) == "" {
			return nil, fmt.Errorf("%w：第%d行观测标识不能为空", ErrInvalid, row+1)
		}
		if observationIDs[o.ID] {
			return nil, fmt.Errorf("%w：第%d行观测标识%s重复", ErrInvalid, row+1, o.ID)
		}
		observationIDs[o.ID] = true
		if strings.TrimSpace(o.ObservedAt) != stage {
			return nil, fmt.Errorf("%w：第%d行不属于同一观测阶段", ErrInvalid, row+1)
		}
		if o.ReplicateNo < 1 || o.ReplicateNo > t.ReplicateCount {
			return nil, fmt.Errorf("%w：第%d行重复组编号%d越界", ErrInvalid, row+1, o.ReplicateNo)
		}
		if groups[o.ReplicateNo] {
			return nil, fmt.Errorf("%w：第%d行重复组编号%d重复", ErrInvalid, row+1, o.ReplicateNo)
		}
		groups[o.ReplicateNo] = true
		if o.GerminatedCount < 0 || o.MoldCount < 0 {
			return nil, fmt.Errorf("%w：第%d行数量不能为负", ErrInvalid, row+1)
		}
		if o.HumidityPercent < 0 || o.HumidityPercent > 100 {
			return nil, fmt.Errorf("%w：第%d行湿度范围无效", ErrInvalid, row+1)
		}
		for _, old := range t.Observations {
			if old.ReplicateNo == o.ReplicateNo && old.ObservedAt == stage {
				return nil, fmt.Errorf("%w：第%d行与已有观测重复", ErrInvalid, row+1)
			}
		}
	}
	newIssues := []Issue{}
	for _, o := range observations {
		o.ObservedAt = stage
		t.Observations = append(t.Observations, o)
		for _, candidate := range EvaluateObservation(DefaultObservationPolicy(), o) {
			candidate.ID = fmt.Sprintf("issue-%s-%d", t.ID, len(t.Issues)+1)
			candidate.ObservationID = o.ID
			t.Issues = append(t.Issues, candidate)
			newIssues = append(newIssues, candidate)
		}
	}
	if t.ReviewState == "approved" {
		t.ReviewState = "draft"
	}
	t.DesignSummary.NextObservation = t.NextObservationStage()
	return newIssues, nil
}

func (t *GerminationTrial) DecideIssue(id, reason, disposition, evidence string) error {
	return t.DecideIssues([]IssueDecision{{IssueID: id, Reason: reason, Disposition: disposition, EvidenceRef: evidence}}, "")
}

type IssueDecision struct{ IssueID, Reason, Disposition, EvidenceRef string }

func (t *GerminationTrial) DecideIssues(decisions []IssueDecision, anchorID string) error {
	if t.FrozenAt != "" {
		return ErrFrozen
	}
	if len(decisions) == 0 {
		return fmt.Errorf("%w：异常决定不能为空", ErrInvalid)
	}
	indexes := map[string]int{}
	observationID := ""
	for row, decision := range decisions {
		if indexes[decision.IssueID] != 0 {
			return fmt.Errorf("%w：第%d项异常标识重复", ErrInvalid, row+1)
		}
		index := -1
		for i := range t.Issues {
			if t.Issues[i].ID == decision.IssueID {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("%w：异常%s不存在于当前试验", ErrInvalid, decision.IssueID)
		}
		issue := t.Issues[index]
		if issue.Status != "open" {
			return fmt.Errorf("%w：异常%s已裁决", ErrInvalid, issue.ID)
		}
		if observationID == "" {
			observationID = issue.ObservationID
		}
		if issue.ObservationID != observationID {
			return fmt.Errorf("%w：合并裁决只能覆盖同一次观测", ErrInvalid)
		}
		if strings.TrimSpace(decision.Reason) == "" || strings.TrimSpace(decision.Disposition) == "" {
			return fmt.Errorf("%w：异常%s的原因分类和处置结论不能为空", ErrInvalid, issue.ID)
		}
		if issue.Kind == "evidence" && strings.TrimSpace(decision.EvidenceRef) == "" {
			return fmt.Errorf("%w：缺证异常%s必须补充证据索引", ErrInvalid, issue.ID)
		}
		indexes[decision.IssueID] = index + 1
	}
	if anchorID != "" {
		if _, ok := indexes[anchorID]; !ok {
			return fmt.Errorf("%w：请求未包含入口异常%s", ErrInvalid, anchorID)
		}
	}
	batchID := fmt.Sprintf("decision-%s-%d", t.ID, time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)
	for _, decision := range decisions {
		i := indexes[decision.IssueID] - 1
		t.Issues[i].Status, t.Issues[i].Reason, t.Issues[i].Disposition = "decided", strings.TrimSpace(decision.Reason), strings.TrimSpace(decision.Disposition)
		t.Issues[i].EvidenceRef, t.Issues[i].DecidedAt, t.Issues[i].DecisionBatchID = strings.TrimSpace(decision.EvidenceRef), now, batchID
	}
	return nil
}

func (t *GerminationTrial) SubmitReview(reviewer, state, comment, remediation string) error {
	return t.SubmitReviewWithItems(reviewer, state, comment, remediation, nil)
}

func (t *GerminationTrial) SubmitReviewWithItems(reviewer, state, comment, remediation string, items []RemediationItem) error {
	if t.FrozenAt != "" {
		return ErrFrozen
	}
	if state != "approved" && state != "returned" {
		return fmt.Errorf("%w：复核状态无效", ErrInvalid)
	}
	if !ReviewTransitionAllowed(t.ReviewState, state) {
		return fmt.Errorf("%w：不允许从%s转换到%s", ErrInvalid, t.ReviewState, state)
	}
	if state == "approved" {
		for _, i := range t.Issues {
			if i.Status == "open" {
				return fmt.Errorf("%w：仍有未裁决异常", ErrInvalid)
			}
		}
		for _, item := range t.RemediationItems {
			if item.Status != "completed" {
				return fmt.Errorf("%w：整改项%s尚未完成", ErrInvalid, item.ID)
			}
		}
	} else {
		if len(items) == 0 {
			return fmt.Errorf("%w：退回复核至少需要一个结构化整改项", ErrInvalid)
		}
		for i := range items {
			if strings.TrimSpace(items[i].Problem) == "" || strings.TrimSpace(items[i].ResponsibleRole) == "" || strings.TrimSpace(items[i].CompletionCriteria) == "" {
				return fmt.Errorf("%w：第%d个整改项的问题说明、责任角色和完成标准不能为空", ErrInvalid, i+1)
			}
			items[i].ID = fmt.Sprintf("rem-%s-%d-%d", t.ID, len(t.ReviewHistory)+1, i+1)
			items[i].Status = "open"
		}
		t.RemediationItems = append(t.RemediationItems, items...)
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(comment) == "" {
		return fmt.Errorf("%w：复核人和意见不能为空", ErrInvalid)
	}
	changeSummary := fmt.Sprintf("本轮版本包含%d条观测、%d项异常、%d项整改", len(t.Observations), len(t.Issues), len(t.RemediationItems))
	t.Review = Review{State: state, Reviewer: reviewer, Comment: comment, Remediation: remediation, ReviewedAt: time.Now().UTC().Format(time.RFC3339), RemediationItems: append([]RemediationItem(nil), items...), ChangeSummary: changeSummary}
	t.ReviewHistory = append(t.ReviewHistory, t.Review)
	t.ReviewState = state
	return nil
}

func (t *GerminationTrial) AddEvidence(note EvidenceNote) error {
	if t.FrozenAt != "" {
		return ErrFrozen
	}
	if strings.TrimSpace(note.Author) == "" || strings.TrimSpace(note.Reference) == "" {
		return fmt.Errorf("%w：证据提交人和索引不能为空", ErrInvalid)
	}
	if note.Kind != "photo" && note.Kind != "document" && note.Kind != "reading" && note.Kind != "remediation" {
		return fmt.Errorf("%w：证据类型无效", ErrInvalid)
	}
	for _, old := range t.EvidenceNotes {
		if old.Reference == note.Reference {
			return fmt.Errorf("%w：证据索引已存在", ErrInvalid)
		}
	}
	if note.CreatedAt == "" {
		note.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if note.RemediationItemID != "" {
		found := false
		for i := range t.RemediationItems {
			if t.RemediationItems[i].ID != note.RemediationItemID {
				continue
			}
			found = true
			if t.RemediationItems[i].Status == "completed" {
				return fmt.Errorf("%w：整改项已完成", ErrInvalid)
			}
			if strings.TrimSpace(note.Reference) == "" {
				return fmt.Errorf("%w：整改证据索引不能为空", ErrInvalid)
			}
			t.RemediationItems[i].Status, t.RemediationItems[i].EvidenceRef = "completed", note.Reference
			t.RemediationItems[i].CompletionNote, t.RemediationItems[i].CompletedAt = strings.TrimSpace(note.Comment), note.CreatedAt
			t.RemediationItems[i].CompletedVersion = t.Version + 1
			for reviewIndex := range t.ReviewHistory {
				for itemIndex := range t.ReviewHistory[reviewIndex].RemediationItems {
					if t.ReviewHistory[reviewIndex].RemediationItems[itemIndex].ID == note.RemediationItemID {
						t.ReviewHistory[reviewIndex].RemediationItems[itemIndex] = t.RemediationItems[i]
					}
				}
			}
			for itemIndex := range t.Review.RemediationItems {
				if t.Review.RemediationItems[itemIndex].ID == note.RemediationItemID {
					t.Review.RemediationItems[itemIndex] = t.RemediationItems[i]
				}
			}
			break
		}
		if !found {
			return fmt.Errorf("%w：整改项%s不存在", ErrInvalid, note.RemediationItemID)
		}
	}
	t.EvidenceNotes = append(t.EvidenceNotes, note)
	if t.ReviewState == "approved" {
		t.ReviewState = "draft"
	}
	return nil
}

func (t *GerminationTrial) Freeze() error {
	if t.FrozenAt != "" {
		return ErrFrozen
	}
	if t.ReviewState != "approved" {
		return fmt.Errorf("%w：只有复核通过的试验可以封存", ErrInvalid)
	}
	if len(t.Observations) == 0 {
		return fmt.Errorf("%w：没有观测记录", ErrInvalid)
	}
	t.FrozenAt = time.Now().UTC().Format(time.RFC3339)
	t.ReviewState = "frozen"
	return nil
}
