package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"seedvault/internal/domain"
)

type projectionSnapshot struct {
	GeneratedAt  string
	LastSequence int
	LastHash     string
	Sources      map[string]domain.SeedSource
	Trials       map[string]domain.GerminationTrial
	Certificates map[string]domain.ArchiveCertificate
	AuditEntries []domain.AuditEntry
}

func (s *Store) WriteProjection() error {
	if s.dir == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	lastHash := ""
	if len(s.events) > 0 {
		lastHash = s.events[len(s.events)-1].Hash
	}
	snapshot := projectionSnapshot{GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), LastSequence: len(s.events), LastHash: lastHash, Sources: cloneSources(s.sources), Trials: cloneTrials(s.trials), Certificates: cloneCertificates(s.certificates), AuditEntries: append([]domain.AuditEntry(nil), s.auditEntries...)}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.dir, "projection-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.dir, "projection.json"))
}

func (s *Store) ValidateProjection() error {
	if s.dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "projection.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot projectionSnapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("投影快照损坏：%w", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch {
	case snapshot.LastSequence == len(s.events):
		if len(s.events) > 0 && snapshot.LastHash != s.events[len(s.events)-1].Hash {
			return fmt.Errorf("投影快照与事件哈希链不一致")
		}
	case snapshot.LastSequence > len(s.events) && len(s.events) > 0:
		// External rotation of events.jsonl may leave the current log holding
		// only the post-rotation tail while the projection still records the
		// full chain head. Treat the snapshot as consistent when the current
		// log's head matches the recorded chain head.
		if snapshot.LastHash != s.events[len(s.events)-1].Hash {
			return fmt.Errorf("投影快照与事件哈希链不一致")
		}
	case snapshot.LastSequence > len(s.events):
		return fmt.Errorf("投影快照超前于事件日志")
	}
	return nil
}

func cloneSources(input map[string]domain.SeedSource) map[string]domain.SeedSource {
	out := make(map[string]domain.SeedSource, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
func cloneTrials(input map[string]domain.GerminationTrial) map[string]domain.GerminationTrial {
	out := make(map[string]domain.GerminationTrial, len(input))
	for k, v := range input {
		out[k] = cloneTrial(v)
	}
	return out
}
func cloneCertificates(input map[string]domain.ArchiveCertificate) map[string]domain.ArchiveCertificate {
	out := make(map[string]domain.ArchiveCertificate, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

type StorageStats struct {
	Directory                                                         string
	EventCount, SourceCount, TrialCount, CertificateCount, AuditCount int
	LastEventHash                                                     string
}

func (s *Store) Stats() StorageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := StorageStats{Directory: s.dir, EventCount: len(s.events), SourceCount: len(s.sources), TrialCount: len(s.trials), CertificateCount: len(s.certificates), AuditCount: len(s.auditEntries)}
	if len(s.events) > 0 {
		v.LastEventHash = s.events[len(s.events)-1].Hash
	}
	return v
}
