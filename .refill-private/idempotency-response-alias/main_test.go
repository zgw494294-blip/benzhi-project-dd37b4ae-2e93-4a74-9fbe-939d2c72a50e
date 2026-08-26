package idempotency_response_alias_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"seedvault/internal/httpui"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

func TestIdempotencyReplayOwnsResponseBytes(t *testing.T) {
	dir := t.TempDir()
	dataStore, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpui.New(workflow.New(dataStore)).Handler()

	firstRequest := []byte(`{"ID":"src-buffer-a","ScientificName":"Abies ziyuanensis","CollectionLocation":"广西资源","Collector":"实验员甲","CollectionDate":"2025-04-10","StorageCondition":"低温干燥","LotCode":"BUF-A"}`)
	firstResponse := postSource(t, handler, "idem-buffer-first", firstRequest)
	stored, ok := dataStore.Idempotency("idem-buffer-first")
	if !ok || len(stored.ResponseBody) == 0 {
		t.Fatal("首次成功请求未写入幂等响应")
	}
	stored.ResponseBody[0] ^= 1
	readReplay := postSource(t, handler, "idem-buffer-first", firstRequest)
	if !bytes.Equal(readReplay, firstResponse) {
		t.Errorf("修改 Store.Idempotency 返回值污染了重放响应")
	}

	secondRequest := []byte(`{"ID":"src-buffer-b","ScientificName":"Taxus chinensis","CollectionLocation":"云南保山","Collector":"实验员乙","CollectionDate":"2025-05-11","StorageCondition":"冷藏避光","LotCode":"BUF-B"}`)
	postSource(t, handler, "idem-buffer-second", secondRequest)
	replayed := postSource(t, handler, "idem-buffer-first", firstRequest)

	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reopened.Idempotency("idem-buffer-first")
	if !ok || !bytes.Equal(persisted.ResponseBody, firstResponse) {
		t.Fatalf("重启后的幂等记录未保留首次响应")
	}
	if !bytes.Equal(replayed, firstResponse) {
		t.Errorf("同进程幂等重放被后续请求的复用缓冲区污染\n首次响应: %s\n重放响应: %s", firstResponse, replayed)
	}
}

func postSource(t *testing.T, handler http.Handler, key string, body []byte) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /api/sources 返回 %d: %s", recorder.Code, recorder.Body.String())
	}
	return append([]byte(nil), recorder.Body.Bytes()...)
}
