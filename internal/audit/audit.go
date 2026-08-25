package audit

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"seedvault/internal/domain"
	"seedvault/internal/store"
)

type Service struct{ Store *store.Store }

type TrailVerification struct {
	Valid       bool
	AggregateID string
	EntryCount  int
	ChainHead   string
	Failures    []string
}

func New(s *store.Store) *Service { return &Service{Store: s} }

func (a *Service) Record(aggregateID, action, actor, role, requestKey string, expectedVersion, resultVersion int, details map[string]string) error {
	entries := a.Store.AuditEntries(aggregateID)
	previous := ""
	sequence := len(entries) + 1
	if len(entries) > 0 {
		previous = entries[len(entries)-1].Hash
	}
	entry := domain.AuditEntry{Sequence: sequence, ID: fmt.Sprintf("audit-%s-%d", aggregateID, sequence), AggregateID: aggregateID, Action: action, Actor: actor, Role: role, RequestKey: requestKey, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), ExpectedVersion: expectedVersion, ResultVersion: resultVersion, Details: details, PreviousHash: previous}
	payload, _ := json.Marshal(struct {
		Sequence                                                     int
		ID, AggregateID, Action, Actor, Role, RequestKey, OccurredAt string
		ExpectedVersion, ResultVersion                               int
		Details                                                      map[string]string
		PreviousHash                                                 string
	}{entry.Sequence, entry.ID, entry.AggregateID, entry.Action, entry.Actor, entry.Role, entry.RequestKey, entry.OccurredAt, entry.ExpectedVersion, entry.ResultVersion, entry.Details, entry.PreviousHash})
	h := sha256.Sum256(append([]byte(previous), payload...))
	entry.Hash = hex.EncodeToString(h[:])
	return a.Store.PutAudit(entry)
}

func (a *Service) Entries(aggregateID string) []domain.AuditEntry {
	return a.Store.AuditEntries(aggregateID)
}

func (a *Service) VerifyTrail(aggregateID string) TrailVerification {
	entries := a.Store.AuditEntries(aggregateID)
	result := TrailVerification{Valid: true, AggregateID: aggregateID, EntryCount: len(entries)}
	previous := ""
	for index, entry := range entries {
		if entry.Sequence != index+1 {
			result.Failures = append(result.Failures, fmt.Sprintf("第%d条记录序号不连续", index+1))
		}
		if entry.PreviousHash != previous {
			result.Failures = append(result.Failures, fmt.Sprintf("第%d条记录前置哈希不匹配", index+1))
		}
		payload, _ := json.Marshal(struct {
			Sequence                                                     int
			ID, AggregateID, Action, Actor, Role, RequestKey, OccurredAt string
			ExpectedVersion, ResultVersion                               int
			Details                                                      map[string]string
			PreviousHash                                                 string
		}{entry.Sequence, entry.ID, entry.AggregateID, entry.Action, entry.Actor, entry.Role, entry.RequestKey, entry.OccurredAt, entry.ExpectedVersion, entry.ResultVersion, entry.Details, entry.PreviousHash})
		hash := sha256.Sum256(append([]byte(previous), payload...))
		if entry.Hash != hex.EncodeToString(hash[:]) {
			result.Failures = append(result.Failures, fmt.Sprintf("第%d条记录内容哈希不匹配", index+1))
		}
		previous = entry.Hash
	}
	result.ChainHead = previous
	result.Valid = len(result.Failures) == 0
	return result
}

type CertificateVerification struct {
	Valid                                                                         bool
	CertificateID, TrialID, RecordedHash, CalculatedHash, IssuedAt, Issuer        string
	RecordedAuditChainHead, CalculatedAuditChainHead                              string
	RecordedFrozenVersion, CurrentFrozenVersion                                   int
	SnapshotHashValid, AuditChainValid, CredentialStatusValid, FrozenVersionValid bool
	Reasons                                                                       []string
}

func (a *Service) VerificationSummary(c domain.ArchiveCertificate, t domain.GerminationTrial) CertificateVerification {
	trail := a.VerifyTrail(t.ID)
	v := CertificateVerification{Valid: true, CertificateID: c.ID, TrialID: c.TrialID, RecordedHash: c.SnapshotHash, CalculatedHash: a.SnapshotHash(t), IssuedAt: c.IssuedAt, Issuer: c.Issuer, RecordedAuditChainHead: c.AuditChainHead, CalculatedAuditChainHead: trail.ChainHead, RecordedFrozenVersion: c.FrozenVersion, CurrentFrozenVersion: t.Version}
	v.SnapshotHashValid = v.RecordedHash == v.CalculatedHash
	v.AuditChainValid = trail.Valid && c.AuditChainHead != "" && c.AuditChainHead == trail.ChainHead
	v.CredentialStatusValid = c.Status == "valid" && c.TrialID == t.ID && t.FrozenAt != ""
	v.FrozenVersionValid = c.FrozenVersion > 0 && c.FrozenVersion == t.Version
	if c.Status != "valid" {
		v.Reasons = append(v.Reasons, "凭据状态无效")
	}
	if c.TrialID != t.ID {
		v.Reasons = append(v.Reasons, "凭据与试验标识不匹配")
	}
	if t.FrozenAt == "" {
		v.Reasons = append(v.Reasons, "试验没有冻结时间")
	}
	if v.RecordedHash != v.CalculatedHash {
		v.Reasons = append(v.Reasons, "快照哈希不匹配")
	}
	if !v.AuditChainValid {
		v.Reasons = append(v.Reasons, "审计链头不匹配或审计链无效")
	}
	if !v.FrozenVersionValid {
		v.Reasons = append(v.Reasons, "冻结版本不匹配")
	}
	v.Valid = len(v.Reasons) == 0
	return v
}
func (a *Service) SnapshotHash(t domain.GerminationTrial) string {
	clone := canonicalSnapshot(t)
	b, _ := json.Marshal(clone)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (a *Service) IssueCertificate(t domain.GerminationTrial, issuer string) (domain.ArchiveCertificate, error) {
	if t.FrozenAt == "" {
		return domain.ArchiveCertificate{}, fmt.Errorf("试验尚未冻结")
	}
	nonce := make([]byte, 12)
	if _, e := rand.Read(nonce); e != nil {
		return domain.ArchiveCertificate{}, e
	}
	trail := a.VerifyTrail(t.ID)
	if !trail.Valid {
		return domain.ArchiveCertificate{}, fmt.Errorf("审计链无效，无法签发凭据")
	}
	c := domain.ArchiveCertificate{ID: "cert-" + t.ID, TrialID: t.ID, SnapshotHash: a.SnapshotHash(t), IssuedAt: time.Now().UTC().Format(time.RFC3339), Issuer: issuer, VerificationNonce: hex.EncodeToString(nonce), Status: "valid", AuditChainHead: trail.ChainHead, FrozenVersion: t.Version}
	if e := a.Store.PutCertificate(c); e != nil {
		return domain.ArchiveCertificate{}, e
	}
	return c, nil
}
func (a *Service) Verify(c domain.ArchiveCertificate, t domain.GerminationTrial) bool {
	return a.VerificationSummary(c, t).Valid
}
