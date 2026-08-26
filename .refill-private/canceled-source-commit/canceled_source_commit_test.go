package canceledsourcecommit_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"seedvault/internal/httpui"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

func TestCanceledSourceRequestDoesNotCommit(t *testing.T) {
	data := `{"ID":"src-canceled","ScientificName":"Davidia involucrata","CollectionLocation":"四川卧龙保护样地","Collector":"实验员","CollectionDate":"2026-08-25","StorageCondition":"4C干燥避光","LotCode":"CANCEL-001"}`
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpui.New(workflow.New(st)).Handler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewBufferString(data)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code < http.StatusBadRequest {
		t.Fatalf("TestCanceledSourceRequestDoesNotCommit: canceled request unexpectedly succeeded with HTTP %d", response.Code)
	}
	if _, ok := st.Source("src-canceled"); ok {
		t.Fatalf("TestCanceledSourceRequestDoesNotCommit: canceled request returned HTTP %d but committed the source", response.Code)
	}
	if st.EventCount() != 0 {
		t.Fatalf("TestCanceledSourceRequestDoesNotCommit: canceled request left %d persisted events", st.EventCount())
	}
}
