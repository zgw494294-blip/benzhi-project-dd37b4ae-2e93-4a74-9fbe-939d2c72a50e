package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"seedvault/internal/domain"
)

type IntegrityFinding struct{ Severity, Code, AggregateID, Message string }
type IntegrityReport struct {
	Valid                                                                                  bool
	CheckedEvents, CheckedSources, CheckedTrials, CheckedCertificates, CheckedAuditEntries int
	ChainHead                                                                              string
	Findings                                                                               []IntegrityFinding
}

func (s *Store) CheckIntegrity() IntegrityReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := IntegrityReport{Valid: true, CheckedEvents: len(s.events), CheckedSources: len(s.sources), CheckedTrials: len(s.trials), CheckedCertificates: len(s.certificates), CheckedAuditEntries: len(s.auditEntries)}
	previous := ""
	for index, item := range s.events {
		raw, _ := json.Marshal(struct {
			Seq      int `json:"seq"`
			Type, ID string
			Version  int
			Payload  json.RawMessage
			PrevHash string
		}{item.Seq, item.Type, item.ID, item.Version, item.Payload, item.PrevHash})
		// The in-memory chain may begin with a continuation event whose
		// PrevHash anchors to a rotated-out head; seed hash verification from
		// the carried pointer so post-rotation appends stay verifiable.
		verifyPrev := previous
		firstAfterRotation := index == 0 && previous == ""
		if firstAfterRotation {
			verifyPrev = item.PrevHash
		}
		hash := sha256.Sum256(append([]byte(verifyPrev), raw...))
		expected := hex.EncodeToString(hash[:])
		// Sequence numbers continue the global chain across a rotation, so the
		// first persisted event in a continuation log keeps its original seq
		// rather than restarting from 1.
		if (!firstAfterRotation || item.PrevHash == "") && item.Seq != index+1 {
			report.add("error", "event.sequence", item.ID, "事件序号不连续")
		}
		// The first event of a continuation log legitimately anchors to a
		// non-empty previous head that is no longer in the current log.
		if !firstAfterRotation && item.PrevHash != previous {
			report.add("error", "event.previous_hash", item.ID, "事件前置哈希不匹配")
		}
		if item.Hash != expected {
			report.add("error", "event.hash", item.ID, "事件内容哈希不匹配")
		}
		previous = item.Hash
	}
	report.ChainHead = previous
	for id, source := range s.sources {
		if source.ID != id {
			report.add("error", "source.identity", id, "种源投影键与实体标识不一致")
		}
		if source.Version < 1 {
			report.add("error", "source.version", id, "种源版本无效")
		}
	}
	for id, trial := range s.trials {
		if trial.ID != id {
			report.add("error", "trial.identity", id, "试验投影键与实体标识不一致")
		}
		if _, ok := s.sources[trial.SeedSourceID]; !ok {
			report.add("error", "trial.source", id, "试验引用的种源不存在")
		}
		if err := trial.ValidateSnapshot(); err != nil {
			report.add("error", "trial.snapshot", id, err.Error())
		}
	}
	for id, certificate := range s.certificates {
		trial, ok := s.trials[certificate.TrialID]
		if !ok {
			report.add("error", "certificate.trial", id, "凭据引用的试验不存在")
			continue
		}
		if trial.FrozenAt == "" {
			report.add("error", "certificate.unfrozen", id, "凭据对应试验未冻结")
		}
		if certificate.Status != "valid" {
			report.add("warning", "certificate.status", id, "凭据状态不是valid")
		}
	}
	last := map[string]domain.AuditEntry{}
	for _, entry := range s.auditEntries {
		old, exists := last[entry.AggregateID]
		if exists && entry.Sequence != old.Sequence+1 {
			report.add("error", "audit.sequence", entry.AggregateID, "审计序号不连续")
		}
		if exists && entry.PreviousHash != old.Hash {
			report.add("error", "audit.previous_hash", entry.AggregateID, "审计前置哈希不匹配")
		}
		if !exists && entry.Sequence != 1 {
			report.add("error", "audit.first_sequence", entry.AggregateID, "首条审计序号不是1")
		}
		last[entry.AggregateID] = entry
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity == report.Findings[j].Severity {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].Severity < report.Findings[j].Severity
	})
	return report
}
func (r *IntegrityReport) add(severity, code, id, message string) {
	r.Findings = append(r.Findings, IntegrityFinding{Severity: severity, Code: code, AggregateID: id, Message: message})
	if severity == "error" {
		r.Valid = false
	}
}
func (r IntegrityReport) Error() error {
	if r.Valid {
		return nil
	}
	return fmt.Errorf("完整性检查发现%d项问题", len(r.Findings))
}
