package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"seedvault/internal/domain"
)

type event struct {
	Seq      int             `json:"seq"`
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Version  int             `json:"version"`
	Payload  json.RawMessage `json:"payload"`
	PrevHash string          `json:"prev_hash"`
	Hash     string          `json:"hash"`
}

type Store struct {
	mu           sync.RWMutex
	dir          string
	sources      map[string]domain.SeedSource
	trials       map[string]domain.GerminationTrial
	certificates map[string]domain.ArchiveCertificate
	auditEntries []domain.AuditEntry
	auditViews   map[string][]domain.AuditEntry
	events       []event
	idem         map[string]json.RawMessage
	idempotency  map[string]IdempotencyRecord
	lotIndex     map[string]string
	sourceTrials map[string]map[string]bool
	designIndex  map[string]string
}

type IdempotencyRecord struct {
	Key, Method, Path, RequestHash string
	Status                         int
	ResponseBody                   []byte
	CreatedAt                      string
}

func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, sources: map[string]domain.SeedSource{}, trials: map[string]domain.GerminationTrial{}, certificates: map[string]domain.ArchiveCertificate{}, auditViews: map[string][]domain.AuditEntry{}, idem: map[string]json.RawMessage{}, idempotency: map[string]IdempotencyRecord{}, lotIndex: map[string]string{}, sourceTrials: map[string]map[string]bool{}, designIndex: map[string]string{}}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	if err := replay(s); err != nil {
		return nil, err
	}
	if err := s.ValidateProjection(); err != nil {
		return nil, err
	}
	return s, nil
}
func replay(s *Store) error {
	f, err := os.Open(filepath.Join(s.dir, "events.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	prev := ""
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			return fmt.Errorf("事件损坏")
		}
		raw, _ := json.Marshal(struct {
			Seq      int `json:"seq"`
			Type, ID string
			Version  int
			Payload  json.RawMessage
			PrevHash string
		}{e.Seq, e.Type, e.ID, e.Version, e.Payload, e.PrevHash})
		h := sha256.Sum256(append([]byte(prev), raw...))
		if hex.EncodeToString(h[:]) != e.Hash {
			return fmt.Errorf("审计哈希链校验失败")
		}
		prev = e.Hash
		s.events = append(s.events, e)
		switch e.Type {
		case "source":
			var v domain.SeedSource
			json.Unmarshal(e.Payload, &v)
			s.sources[e.ID] = v
			s.lotIndex[domain.NormalizeLotCode(v.LotCode)] = e.ID
		case "trial":
			var v domain.GerminationTrial
			json.Unmarshal(e.Payload, &v)
			s.trials[e.ID] = v
		case "certificate":
			var v domain.ArchiveCertificate
			json.Unmarshal(e.Payload, &v)
			s.certificates[e.ID] = v
		case "audit":
			var v domain.AuditEntry
			json.Unmarshal(e.Payload, &v)
			s.auditEntries = append(s.auditEntries, v)
		case "idempotency":
			var v IdempotencyRecord
			json.Unmarshal(e.Payload, &v)
			s.idempotency[v.Key] = v
		}
	}
	s.rebuildTrialIndexesLocked()
	return sc.Err()
}
func (s *Store) appendLocked(typ, id string, version int, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	prev := ""
	if len(s.events) > 0 {
		prev = s.events[len(s.events)-1].Hash
	}
	e := event{Seq: len(s.events) + 1, Type: typ, ID: id, Version: version, Payload: b, PrevHash: prev}
	raw, _ := json.Marshal(struct {
		Seq      int `json:"seq"`
		Type, ID string
		Version  int
		Payload  json.RawMessage
		PrevHash string
	}{e.Seq, e.Type, e.ID, e.Version, e.Payload, e.PrevHash})
	h := sha256.Sum256(append([]byte(prev), raw...))
	e.Hash = hex.EncodeToString(h[:])
	s.events = append(s.events, e)
	if s.dir != "" {
		f, er := os.OpenFile(filepath.Join(s.dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if er != nil {
			return er
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		if er = enc.Encode(e); er != nil {
			return er
		}
	}
	return nil
}
func (s *Store) PutSource(v domain.SeedSource, expected int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if oldID, ok := s.lotIndex[domain.NormalizeLotCode(v.LotCode)]; ok && oldID != v.ID {
		old := s.sources[oldID]
		return fmt.Errorf("%w：批次编码%s已存在于档案%s（当前版本%d）", domain.ErrConflict, domain.NormalizeLotCode(v.LotCode), old.ID, old.Version)
	}
	if old, ok := s.sources[v.ID]; ok && old.Version != expected {
		return domain.ErrConflict
	}
	if !okExpected(s.sources, v.ID, expected) {
		return domain.ErrConflict
	}
	v.Version = expected + 1
	v.LotCode = domain.NormalizeLotCode(v.LotCode)
	v.AssociatedTrialCount, v.UnarchivedTrialCount = 0, 0
	v.CanCreateTrial = v.Status == "active"
	s.sources[v.ID] = v
	s.lotIndex[v.LotCode] = v.ID
	return s.appendLocked("source", v.ID, v.Version, v)
}
func okExpected[T any](m map[string]T, id string, expected int) bool {
	_, ok := m[id]
	if !ok {
		return expected == 0
	}
	return true
}
func (s *Store) PutTrial(v domain.GerminationTrial, expected int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.trials[v.ID]
	if ok && old.Version != expected {
		return domain.ErrConflict
	}
	if !ok && expected != 0 {
		return domain.ErrConflict
	}
	v.Version = expected + 1
	s.trials[v.ID] = cloneTrial(v)
	s.rebuildTrialIndexesLocked()
	return s.appendLocked("trial", v.ID, v.Version, v)
}
func (s *Store) PutCertificate(v domain.ArchiveCertificate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.certificates[v.ID]; ok {
		return domain.ErrConflict
	}
	s.certificates[v.ID] = v
	return s.appendLocked("certificate", v.ID, 1, v)
}

func (s *Store) PutAudit(v domain.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditEntries = append(s.auditEntries, v)
	delete(s.auditViews, v.AggregateID)
	delete(s.auditViews, "")
	return s.appendLocked("audit", v.ID, v.ResultVersion, v)
}

func (s *Store) AuditEntries(aggregateID string) []domain.AuditEntry {
	s.mu.RLock()
	if cached, ok := s.auditViews[aggregateID]; ok {
		out := cloneAuditEntries(cached)
		s.mu.RUnlock()
		return out
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.auditViews[aggregateID]; ok {
		return cloneAuditEntries(cached)
	}
	result := make([]domain.AuditEntry, 0, len(s.auditEntries))
	for _, item := range s.auditEntries {
		if aggregateID == "" || item.AggregateID == aggregateID {
			result = append(result, item)
		}
	}
	cached := cloneAuditEntries(result)
	s.auditViews[aggregateID] = cached
	return cloneAuditEntries(cached)
}

func (s *Store) DataDir() string { return s.dir }
func (s *Store) Source(id string) (domain.SeedSource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sources[id]
	if ok {
		v = s.decorateSourceLocked(v)
	}
	return v, ok
}

func (s *Store) SourceByLot(lot string) (domain.SeedSource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.lotIndex[domain.NormalizeLotCode(lot)]
	if !ok {
		return domain.SeedSource{}, false
	}
	return s.decorateSourceLocked(s.sources[id]), true
}
func (s *Store) Trial(id string) (domain.GerminationTrial, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.trials[id]
	return cloneTrial(v), ok
}
func (s *Store) Sources() []domain.SeedSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.SeedSource, 0, len(s.sources))
	for _, v := range s.sources {
		out = append(out, s.decorateSourceLocked(v))
	}
	return out
}
func (s *Store) Trials() []domain.GerminationTrial {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.GerminationTrial, 0, len(s.trials))
	for _, v := range s.trials {
		out = append(out, cloneTrial(v))
	}
	return out
}
func (s *Store) Certificate(id string) (domain.ArchiveCertificate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.certificates[id]
	return v, ok
}
func (s *Store) FindCertificateByTrial(id string) (domain.ArchiveCertificate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.certificates {
		if v.TrialID == id {
			return v, true
		}
	}
	return domain.ArchiveCertificate{}, false
}
func (s *Store) Idempotent(key string, result any) (bool, error) {
	if key == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.idem[key]; ok {
		return true, json.Unmarshal(b, result)
	}
	b, e := json.Marshal(result)
	if e == nil {
		s.idem[key] = b
	}
	return false, e
}

func (s *Store) Idempotency(key string) (IdempotencyRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.idempotency[key]
	return v, ok
}
func (s *Store) PutIdempotency(record IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.idempotency[record.Key]; ok {
		if old.RequestHash != record.RequestHash {
			return domain.ErrConflict
		}
		return nil
	}
	s.idempotency[record.Key] = record
	return s.appendLocked("idempotency", record.Key, 1, record)
}
func (s *Store) EventCount() int { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.events) }

func (s *Store) ActiveDesignConflict(sourceID, designKey string) (domain.GerminationTrial, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.designIndex[sourceID+"\x00"+designKey]
	if !ok {
		return domain.GerminationTrial{}, false
	}
	return cloneTrial(s.trials[id]), true
}

func (s *Store) rebuildTrialIndexesLocked() {
	s.sourceTrials, s.designIndex = map[string]map[string]bool{}, map[string]string{}
	for id, trial := range s.trials {
		if s.sourceTrials[trial.SeedSourceID] == nil {
			s.sourceTrials[trial.SeedSourceID] = map[string]bool{}
		}
		s.sourceTrials[trial.SeedSourceID][id] = true
		if trial.FrozenAt == "" {
			s.designIndex[trial.SeedSourceID+"\x00"+domain.TrialDesignKey(trial)] = id
		}
	}
}

func (s *Store) decorateSourceLocked(v domain.SeedSource) domain.SeedSource {
	v.AssociatedTrialCount, v.UnarchivedTrialCount = 0, 0
	for id := range s.sourceTrials[v.ID] {
		v.AssociatedTrialCount++
		if s.trials[id].FrozenAt == "" {
			v.UnarchivedTrialCount++
		}
	}
	v.CanCreateTrial = v.Status == "active"
	return v
}
