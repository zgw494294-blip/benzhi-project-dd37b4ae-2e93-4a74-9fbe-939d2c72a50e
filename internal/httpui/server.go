package httpui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"seedvault/internal/domain"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

//go:embed static/*
var assets embed.FS

type Server struct {
	Workflow        *workflow.Service
	mux             *http.ServeMux
	responseWriters chan *bufferedWriter
}

func New(w *workflow.Service) *Server {
	s := &Server{Workflow: w, mux: http.NewServeMux(), responseWriters: make(chan *bufferedWriter, 1)}
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler { return recoverer(s.idempotency(s.mux)) }
func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.IndexHandler)
	static, _ := fs.Sub(assets, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /api/health", s.HealthHandler)
	s.mux.HandleFunc("GET /api/dashboard", s.DashboardHandler)
	s.mux.HandleFunc("GET /api/integrity", s.IntegrityHandler)
	s.mux.HandleFunc("GET /api/sources", s.ListSourcesHandler)
	s.mux.HandleFunc("POST /api/sources", s.CreateSourceHandler)
	s.mux.HandleFunc("POST /api/sources/{id}/status", s.UpdateSourceStatusHandler)
	s.mux.HandleFunc("PATCH /api/sources/{id}", s.UpdateSourceStatusHandler)
	s.mux.HandleFunc("GET /api/trials", s.ListTrialsHandler)
	s.mux.HandleFunc("POST /api/trials", s.CreateTrialHandler)
	s.mux.HandleFunc("GET /api/trials/{id}", s.GetTrialHandler)
	s.mux.HandleFunc("GET /api/trials/{id}/audit", s.AuditHandler)
	s.mux.HandleFunc("POST /api/trials/{id}/observations", s.AddObservationHandler)
	s.mux.HandleFunc("POST /api/trials/{id}/evidence", s.AddEvidenceHandler)
	s.mux.HandleFunc("POST /api/trials/{id}/issues/{issueID}/decision", s.DecideIssueHandler)
	s.mux.HandleFunc("POST /api/trials/{id}/review", s.ReviewHandler)
	s.mux.HandleFunc("POST /api/trials/{id}/freeze", s.FreezeHandler)
	s.mux.HandleFunc("GET /api/certificates/{id}", s.GetCertificateHandler)
	s.mux.HandleFunc("GET /api/certificates/{id}/verify", s.VerifyCertificateHandler)
}
func (s *Server) IndexHandler(w http.ResponseWriter, r *http.Request) {
	b, e := assets.ReadFile("static/index.html")
	if e != nil {
		http.Error(w, "页面不可用", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"status": "ok", "events": s.Workflow.Store.EventCount()})
}
func (s *Server) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	sources := s.Workflow.ListSources()
	trials := s.Workflow.ListTrials()
	open, frozen := 0, 0
	for _, t := range trials {
		if t.FrozenAt != "" {
			frozen++
		}
		for _, i := range t.Issues {
			if i.Status == "open" {
				open++
			}
		}
	}
	write(w, 200, map[string]any{"sourceCount": len(sources), "trialCount": len(trials), "openIssueCount": open, "frozenCount": frozen, "sources": sources, "trials": trials})
}
func (s *Server) IntegrityHandler(w http.ResponseWriter, r *http.Request) {
	report := s.Workflow.IntegrityReport()
	status := 200
	if !report.Valid {
		status = 503
	}
	write(w, status, report)
}
func (s *Server) ListSourcesHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	write(w, 200, s.Workflow.QuerySources(store.SourceFilter{ScientificName: q.Get("scientificName"), Location: q.Get("location"), LotCode: q.Get("lotCode"), Status: q.Get("status"), Offset: queryInt(q.Get("offset")), Limit: queryInt(q.Get("limit"))}))
}
func (s *Server) CreateSourceHandler(w http.ResponseWriter, r *http.Request) {
	var in workflow.SourceInput
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Workflow.CreateSource(in)
	respond(w, v, e, 201)
}
func (s *Server) UpdateSourceStatusHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status          string
		ExpectedVersion int
	}
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Workflow.UpdateSourceStatus(r.PathValue("id"), in.Status, in.ExpectedVersion)
	respond(w, v, e, 200)
}
func (s *Server) ListTrialsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	write(w, 200, s.Workflow.QueryTrials(store.TrialFilter{SeedSourceID: q.Get("sourceID"), ReviewState: q.Get("state"), ProtocolContains: q.Get("protocol"), HasOpenIssues: q.Get("openIssues") == "true", OnlyFrozen: q.Get("frozen") == "true", Offset: queryInt(q.Get("offset")), Limit: queryInt(q.Get("limit"))}))
}
func (s *Server) CreateTrialHandler(w http.ResponseWriter, r *http.Request) {
	var in workflow.TrialInput
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Workflow.CreateTrial(in)
	respond(w, v, e, 201)
}
func (s *Server) GetTrialHandler(w http.ResponseWriter, r *http.Request) {
	v, ok := s.Workflow.GetTrial(r.PathValue("id"))
	if !ok {
		fail(w, domain.ErrNotFound)
		return
	}
	write(w, 200, map[string]any{"trial": v, "summary": v.Summary(), "stages": v.StageSummaries(), "replicates": v.ReplicateSummaries(), "readiness": v.Readiness(), "nextObservation": v.NextObservationStage(), "designSummary": v.DesignSummary, "issueGroups": v.IssueGroups(), "archiveChecklist": v.ArchiveIntegrity(), "lastReturnedReview": v.LastReturnedReview()})
}
func (s *Server) AuditHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	write(w, 200, map[string]any{"entries": s.Workflow.AuditEntries(id), "verification": s.Workflow.VerifyAuditTrail(id)})
}
func (s *Server) AddObservationHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		workflow.ObservationInput
		Observations    []workflow.ObservationInput
		ExpectedVersion int
	}
	if !decode(w, r, &in) {
		return
	}
	inputs := in.Observations
	if len(inputs) == 0 {
		inputs = []workflow.ObservationInput{in.ObservationInput}
	}
	for i := range inputs {
		if inputs[i].ObservedAt == "" {
			inputs[i].ObservedAt = in.ObservedAt
		}
	}
	v, issues, e := s.Workflow.AddObservations(r.PathValue("id"), inputs, in.ExpectedVersion)
	created := len(issues) > 0
	respond(w, map[string]any{"trial": v, "id": v.ID, "version": v.Version, "Version": v.Version, "issues": issues, "issue": firstIssue(issues), "issueCreated": created, "summary": v.Summary(), "stageSummaries": v.StageSummaries(), "nextObservation": v.NextObservationStage()}, e, 201)
}
func (s *Server) AddEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		workflow.EvidenceInput
		ExpectedVersion int
	}
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Workflow.AddEvidence(r.PathValue("id"), in.EvidenceInput, in.ExpectedVersion)
	respond(w, v, e, 201)
}
func (s *Server) DecideIssueHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason, Disposition, Evidence string
		ExpectedVersion               int
		Decisions                     []workflow.DecisionInput
	}
	if !decode(w, r, &in) {
		return
	}
	decisions := in.Decisions
	if len(decisions) == 0 {
		decisions = []workflow.DecisionInput{{IssueID: r.PathValue("issueID"), Reason: in.Reason, Disposition: in.Disposition, Evidence: in.Evidence}}
	}
	v, e := s.Workflow.DecideIssues(r.PathValue("id"), r.PathValue("issueID"), decisions, in.ExpectedVersion)
	respond(w, map[string]any{"trial": v, "id": v.ID, "version": v.Version, "Version": v.Version, "issues": v.Issues, "remainingOpen": v.OpenIssueCount()}, e, 200)
}
func (s *Server) ReviewHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		workflow.ReviewInput
		ExpectedVersion int
	}
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Workflow.ReviewWithItems(r.PathValue("id"), in.ReviewInput, in.ExpectedVersion)
	respond(w, v, e, 200)
}
func (s *Server) FreezeHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issuer          string
		ExpectedVersion int
	}
	if !decode(w, r, &in) {
		return
	}
	v, c, e := s.Workflow.Freeze(r.PathValue("id"), in.Issuer, in.ExpectedVersion)
	if e != nil {
		if current, ok := s.Workflow.GetTrial(r.PathValue("id")); ok {
			write(w, errorStatus(e), map[string]any{"error": e.Error(), "trial": current, "archiveChecklist": current.ArchiveIntegrity()})
			return
		}
		respond(w, nil, e, 201)
		return
	}
	receipt, _, verifyErr := s.Workflow.VerifyReceipt(c.ID)
	respond(w, map[string]any{"trial": v, "certificate": c, "archiveChecklist": v.ArchiveIntegrity(), "verification": receipt}, verifyErr, 201)
}
func (s *Server) GetCertificateHandler(w http.ResponseWriter, r *http.Request) {
	v, ok := s.Workflow.GetCertificate(r.PathValue("id"))
	if !ok {
		fail(w, domain.ErrNotFound)
		return
	}
	write(w, 200, v)
}
func (s *Server) VerifyCertificateHandler(w http.ResponseWriter, r *http.Request) {
	receipt, c, e := s.Workflow.VerifyReceipt(r.PathValue("id"))
	respond(w, map[string]any{"valid": receipt.Valid, "certificate": c, "verification": receipt}, e, 200)
}

type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedWriter) Header() http.Header { return w.header }
func (w *bufferedWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *bufferedWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(data)
}

func (s *Server) acquireBufferedWriter() *bufferedWriter {
	select {
	case writer := <-s.responseWriters:
		writer.header = make(http.Header)
		writer.status = 0
		writer.body.Reset()
		return writer
	default:
		return &bufferedWriter{header: make(http.Header)}
	}
}

func (s *Server) releaseBufferedWriter(writer *bufferedWriter) {
	select {
	case s.responseWriters <- writer:
	default:
	}
}

func (s *Server) idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) < 8 || len(key) > 128 {
			write(w, 400, map[string]string{"error": "Idempotency-Key 长度应为8至128"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			write(w, 400, map[string]string{"error": "请求体读取失败"})
			return
		}
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
		requestHash := fmt.Sprintf("%x", sum[:])
		if old, ok := s.Workflow.Store.Idempotency(key); ok {
			if old.RequestHash != requestHash {
				write(w, 409, map[string]string{"error": "幂等键已用于不同请求"})
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(old.Status)
			w.Write(old.ResponseBody)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		captured := s.acquireBufferedWriter()
		defer s.releaseBufferedWriter(captured)
		next.ServeHTTP(captured, r)
		for name, values := range captured.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		status := captured.status
		if status == 0 {
			status = 200
		}
		response := captured.body.Bytes()
		if status >= 200 && status < 300 {
			record := store.IdempotencyRecord{Key: key, Method: r.Method, Path: r.URL.Path, RequestHash: requestHash, Status: status, ResponseBody: response, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
			if err := s.Workflow.Store.PutIdempotency(record); err != nil {
				fail(w, err)
				return
			}
		}
		w.WriteHeader(status)
		w.Write(response)
	})
}
