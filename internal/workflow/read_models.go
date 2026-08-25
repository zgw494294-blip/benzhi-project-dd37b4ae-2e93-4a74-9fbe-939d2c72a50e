package workflow

import (
	"fmt"

	"seedvault/internal/audit"
	"seedvault/internal/domain"
	"seedvault/internal/store"
)

func (s *Service) GetTrial(id string) (domain.GerminationTrial, bool) {
	return s.Store.Trial(id)
}

func (s *Service) ListSources() []domain.SeedSource {
	return s.Store.Sources()
}

func (s *Service) ListTrials() []domain.GerminationTrial {
	return s.Store.Trials()
}

func (s *Service) QueryTrials(filter store.TrialFilter) store.TrialPage {
	return s.Store.QueryTrials(filter)
}

func (s *Service) QuerySources(filter store.SourceFilter) store.SourcePage {
	return s.Store.QuerySources(filter)
}

func (s *Service) GetCertificate(id string) (domain.ArchiveCertificate, bool) {
	if certificate, ok := s.Store.Certificate(id); ok {
		return certificate, true
	}
	return s.Store.FindCertificateByTrial(id)
}

func (s *Service) Verify(id string) (bool, domain.ArchiveCertificate, error) {
	verification, certificate, err := s.VerifyReceipt(id)
	return verification.Valid, certificate, err
}

func (s *Service) VerifyReceipt(id string) (audit.CertificateVerification, domain.ArchiveCertificate, error) {
	certificate, ok := s.GetCertificate(id)
	if !ok {
		return audit.CertificateVerification{}, certificate, fmt.Errorf("%w：未知封存凭据%s", domain.ErrNotFound, id)
	}
	trial, ok := s.Store.Trial(certificate.TrialID)
	if !ok {
		return audit.CertificateVerification{}, certificate, fmt.Errorf("%w：凭据对应的试验数据不存在", domain.ErrNotFound)
	}
	return s.Audit.VerificationSummary(certificate, trial), certificate, nil
}

func (s *Service) AuditEntries(id string) []domain.AuditEntry {
	return s.Audit.Entries(id)
}

func (s *Service) VerifyAuditTrail(id string) audit.TrailVerification {
	return s.Audit.VerifyTrail(id)
}

func (s *Service) StorageStats() store.StorageStats {
	return s.Store.Stats()
}

func (s *Service) IntegrityReport() store.IntegrityReport {
	return s.Store.CheckIntegrity()
}
