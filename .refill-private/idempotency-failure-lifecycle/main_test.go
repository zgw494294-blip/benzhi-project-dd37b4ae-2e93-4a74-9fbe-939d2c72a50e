package idempotency_failure_lifecycle_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"seedvault/internal/httpui"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

func TestFailedIdempotentRequestCanRetryAfterStateChange(t *testing.T) {
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpui.New(workflow.New(st)).Handler())
	defer server.Close()

	trialBody := []byte(`{"ID":"trial-retry","SourceID":"source-later","ProtocolName":"恢复后重试协议","ReplicateCount":1,"TemperatureCelsius":22,"ObservationSchedule":["第3天"]}`)
	status := post(t, server.URL+"/api/trials", trialBody, "trial-retry-key")
	if status != http.StatusNotFound {
		t.Fatalf("首次请求应因种源尚不存在返回 404，得到 %d", status)
	}

	sourceBody := []byte(`{"ID":"source-later","ScientificName":"Davidia involucrata","CollectionLocation":"四川保护样地","Collector":"实验员","CollectionDate":"2026-08-25","StorageCondition":"4C","LotCode":"RETRY-001"}`)
	if status = post(t, server.URL+"/api/sources", sourceBody, ""); status != http.StatusCreated {
		t.Fatalf("补建种源应成功，得到 %d", status)
	}

	status = post(t, server.URL+"/api/trials", trialBody, "trial-retry-key")
	if status != http.StatusCreated {
		if _, ok := st.Trial("trial-retry"); ok {
			t.Fatalf("相同幂等请求重试应返回 201，得到 %d，且不应依赖已写入的试验", status)
		}
		t.Fatalf("相同幂等请求在依赖状态恢复后应重新执行并返回 201，得到 %d", status)
	}
}

func post(t *testing.T, url string, body []byte, key string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
