package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"seedvault/internal/audit"
	"seedvault/internal/domain"
	"seedvault/internal/store"
)

type Service struct {
	Store *store.Store
	Audit *audit.Service
	mu    sync.Mutex
}

func New(s *store.Store) *Service { return &Service{Store: s, Audit: audit.New(s)} }

type SourceInput struct{ ID, ScientificName, CollectionLocation, Collector, CollectionDate, StorageCondition, LotCode, Status string }
type TrialInput struct {
	ID, SourceID, ProtocolName string
	ReplicateCount             int
	TemperatureCelsius         float64
	ObservationSchedule        []string
}
type ObservationInput struct {
	ID, ObservedAt                          string
	ReplicateNo, GerminatedCount, MoldCount int
	TemperatureCelsius, HumidityPercent     float64
	EvidenceRef                             string
}
type EvidenceInput struct{ ID, Author, Kind, Reference, Comment, RemediationItemID string }
type RemediationItemInput struct{ Problem, ResponsibleRole, CompletionCriteria string }
type ReviewInput struct {
	Reviewer, State, Comment, Remediation string
	RemediationItems                      []RemediationItemInput
}
type DecisionInput struct{ IssueID, Reason, Disposition, Evidence, EvidenceRef string }

func (s *Service) CreateSource(in SourceInput) (domain.SeedSource, error) {
	return s.CreateSourceContext(context.Background(), in)
}

func (s *Service) CreateSourceContext(ctx context.Context, in SourceInput) (domain.SeedSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.Store.SourceByLot(in.LotCode); ok {
		return domain.SeedSource{}, fmt.Errorf("%w：批次编码%s已存在于档案%s（当前版本%d）", domain.ErrConflict, domain.NormalizeLotCode(in.LotCode), old.ID, old.Version)
	}
	if in.ID == "" {
		in.ID = newID("src")
	}
	v, e := domain.NewSeedSource(in.ID, in.ScientificName, in.CollectionLocation, in.Collector, in.CollectionDate, in.StorageCondition, in.LotCode)
	if e != nil {
		return v, e
	}
	if in.Status != "" {
		v.Status, e = domain.NormalizeSourceStatus(in.Status)
		if e != nil {
			return v, e
		}
		v.CanCreateTrial = v.Status == "active"
	}
	e = s.Store.PutSource(v, 0)
	if e == nil && ctx.Err() != nil {
		return v, fmt.Errorf("请求已取消：%w", ctx.Err())
	}
	if e == nil {
		e = s.Audit.Record(v.ID, "source.created", in.Collector, "实验员", "", 0, v.Version, map[string]string{"lotCode": v.LotCode})
		_ = s.Store.WriteProjection()
	}
	return v, e
}

func (s *Service) UpdateSourceStatus(id, status string, expected int) (domain.SeedSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.Store.Source(id)
	if !ok {
		return v, domain.ErrNotFound
	}
	if v.Version != expected {
		return v, domain.ErrConflict
	}
	normalized, err := domain.NormalizeSourceStatus(status)
	if err != nil {
		return v, err
	}
	v.Status, v.CanCreateTrial = normalized, normalized == "active"
	if err = s.Store.PutSource(v, expected); err == nil {
		v.Version = expected + 1
		_ = s.Audit.Record(v.ID, "source.status_changed", "种质库管理员", "管理员", "", expected, v.Version, map[string]string{"status": normalized})
		_ = s.Store.WriteProjection()
	}
	return v, err
}
func (s *Service) CreateTrial(in TrialInput) (domain.GerminationTrial, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.Store.Source(in.SourceID)
	if !ok {
		return domain.GerminationTrial{}, domain.ErrNotFound
	}
	if err := domain.ValidateSourceUsable(source); err != nil {
		return domain.GerminationTrial{}, err
	}
	if in.ID == "" {
		in.ID = newID("trial")
	}
	v, e := domain.NewTrial(in.ID, in.SourceID, in.ProtocolName, in.ReplicateCount, in.TemperatureCelsius, in.ObservationSchedule)
	if e != nil {
		return v, e
	}
	if old, ok := s.Store.ActiveDesignConflict(in.SourceID, domain.TrialDesignKey(v)); ok {
		return domain.GerminationTrial{}, fmt.Errorf("%w：同一种源已有相同协议、温度和时间表的未封存试验%s（状态%s，版本%d）", domain.ErrConflict, old.ID, old.ReviewState, old.Version)
	}
	e = s.Store.PutTrial(v, 0)
	if e == nil {
		e = s.Audit.Record(v.ID, "trial.created", "系统用户", "实验员", "", 0, v.Version, map[string]string{"protocol": v.ProtocolName})
		_ = s.Store.WriteProjection()
	}
	return v, e
}
func (s *Service) AddObservation(id string, in ObservationInput, expected int) (domain.GerminationTrial, domain.Issue, bool, error) {
	t, issues, err := s.AddObservations(id, []ObservationInput{in}, expected)
	if err != nil || len(issues) == 0 {
		return t, domain.Issue{}, false, err
	}
	return t, issues[0], true, nil
}

func (s *Service) AddObservations(id string, inputs []ObservationInput, expected int) (domain.GerminationTrial, []domain.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Store.Trial(id)
	if !ok {
		return t, nil, domain.ErrNotFound
	}
	if t.Version != expected {
		return t, nil, domain.ErrConflict
	}
	observations := make([]domain.Observation, len(inputs))
	for i, in := range inputs {
		if in.ID == "" {
			in.ID = fmt.Sprintf("%s-%d", newID("obs"), i+1)
		}
		stage, _, err := domain.NormalizeObservationStage(in.ObservedAt)
		if err != nil {
			return t, nil, fmt.Errorf("第%d行：%w", i+1, err)
		}
		observations[i] = domain.Observation{ID: in.ID, TrialID: id, ObservedAt: stage, ReplicateNo: in.ReplicateNo, GerminatedCount: in.GerminatedCount, MoldCount: in.MoldCount, TemperatureCelsius: in.TemperatureCelsius, HumidityPercent: in.HumidityPercent, EvidenceRef: strings.TrimSpace(in.EvidenceRef)}
	}
	issues, e := t.AddObservations(observations)
	if e != nil {
		return t, nil, e
	}
	e = s.Store.PutTrial(t, expected)
	if e == nil {
		t.Version = expected + 1
		_ = s.Audit.Record(t.ID, "observation.batch_added", "实验员", "实验员", "", expected, t.Version, map[string]string{"rowCount": strconv.Itoa(len(observations)), "stage": observations[0].ObservedAt, "issueCount": strconv.Itoa(len(issues))})
		_ = s.Store.WriteProjection()
	}
	return t, issues, e
}
func (s *Service) DecideIssue(trialID, issueID, reason, disposition, evidence string, expected int) (domain.GerminationTrial, error) {
	return s.DecideIssues(trialID, issueID, []DecisionInput{{IssueID: issueID, Reason: reason, Disposition: disposition, Evidence: evidence}}, expected)
}

func (s *Service) DecideIssues(trialID, anchorID string, inputs []DecisionInput, expected int) (domain.GerminationTrial, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Store.Trial(trialID)
	if !ok {
		return t, domain.ErrNotFound
	}
	if t.Version != expected {
		return t, domain.ErrConflict
	}
	decisions := make([]domain.IssueDecision, len(inputs))
	ids := make([]string, len(inputs))
	for i, input := range inputs {
		if input.IssueID == "" && len(inputs) == 1 {
			input.IssueID = anchorID
		}
		evidence := input.EvidenceRef
		if evidence == "" {
			evidence = input.Evidence
		}
		decisions[i] = domain.IssueDecision{IssueID: input.IssueID, Reason: input.Reason, Disposition: input.Disposition, EvidenceRef: evidence}
		ids[i] = input.IssueID
	}
	if e := t.DecideIssues(decisions, anchorID); e != nil {
		return t, e
	}
	e := s.Store.PutTrial(t, expected)
	if e == nil {
		t.Version = expected + 1
		_ = s.Audit.Record(t.ID, "issue.batch_decided", "质量复核员", "复核员", "", expected, t.Version, map[string]string{"issueIDs": strings.Join(ids, ","), "issueCount": strconv.Itoa(len(ids)), "observationID": observationForIssue(t, anchorID)})
		_ = s.Store.WriteProjection()
	}
	return t, e
}
func (s *Service) Review(trialID, reviewer, state, comment, remediation string, expected int) (domain.GerminationTrial, error) {
	return s.ReviewWithItems(trialID, ReviewInput{Reviewer: reviewer, State: state, Comment: comment, Remediation: remediation}, expected)
}

func (s *Service) ReviewWithItems(trialID string, in ReviewInput, expected int) (domain.GerminationTrial, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Store.Trial(trialID)
	if !ok {
		return t, domain.ErrNotFound
	}
	if t.Version != expected {
		return t, domain.ErrConflict
	}
	items := make([]domain.RemediationItem, len(in.RemediationItems))
	for i, item := range in.RemediationItems {
		items[i] = domain.RemediationItem{Problem: item.Problem, ResponsibleRole: item.ResponsibleRole, CompletionCriteria: item.CompletionCriteria}
	}
	if e := t.SubmitReviewWithItems(in.Reviewer, in.State, in.Comment, in.Remediation, items); e != nil {
		return t, e
	}
	e := s.Store.PutTrial(t, expected)
	if e == nil {
		t.Version = expected + 1
		_ = s.Audit.Record(t.ID, "review.submitted", in.Reviewer, "复核员", "", expected, t.Version, map[string]string{"state": in.State, "remediationItemCount": strconv.Itoa(len(items))})
		_ = s.Store.WriteProjection()
	}
	return t, e
}

func (s *Service) AddEvidence(trialID string, in EvidenceInput, expected int) (domain.GerminationTrial, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Store.Trial(trialID)
	if !ok {
		return t, domain.ErrNotFound
	}
	if t.Version != expected {
		return t, domain.ErrConflict
	}
	if in.ID == "" {
		in.ID = newID("evidence")
	}
	if err := t.AddEvidence(domain.EvidenceNote{ID: in.ID, Author: in.Author, Kind: in.Kind, Reference: in.Reference, Comment: in.Comment, RemediationItemID: in.RemediationItemID}); err != nil {
		return t, err
	}
	err := s.Store.PutTrial(t, expected)
	if err == nil {
		t.Version = expected + 1
		_ = s.Audit.Record(trialID, "evidence.added", in.Author, "实验员", "", expected, t.Version, map[string]string{"kind": in.Kind, "reference": in.Reference, "remediationItemID": in.RemediationItemID})
		_ = s.Store.WriteProjection()
	}
	return t, err
}
func (s *Service) Freeze(trialID, issuer string, expected int) (domain.GerminationTrial, domain.ArchiveCertificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Store.Trial(trialID)
	if !ok {
		return t, domain.ArchiveCertificate{}, domain.ErrNotFound
	}
	if t.FrozenAt != "" {
		if t.Version != expected {
			return t, domain.ArchiveCertificate{}, domain.ErrConflict
		}
		if c, exists := s.Store.FindCertificateByTrial(trialID); exists {
			return t, c, nil
		}
		return t, domain.ArchiveCertificate{}, fmt.Errorf("%w：试验已冻结但凭据不存在", domain.ErrInvalid)
	}
	if t.Version != expected {
		return t, domain.ArchiveCertificate{}, domain.ErrConflict
	}
	if checklist := t.ArchiveIntegrity(); !checklist.Valid {
		return t, domain.ArchiveCertificate{}, fmt.Errorf("%w：封存完整性检查失败：%s", domain.ErrInvalid, strings.Join(checklist.Reasons, "；"))
	}
	if e := t.Freeze(); e != nil {
		return t, domain.ArchiveCertificate{}, e
	}
	if e := s.Store.PutTrial(t, expected); e != nil {
		return t, domain.ArchiveCertificate{}, e
	}
	t.Version = expected + 1
	_ = s.Audit.Record(t.ID, "trial.frozen", issuer, "签发员", "", expected, t.Version, map[string]string{"certificateID": "cert-" + t.ID})
	_ = s.Store.WriteProjection()
	c, e := s.Audit.IssueCertificate(t, issuer)
	if e == nil {
		_ = s.Store.WriteProjection()
	}
	return t, c, e
}
