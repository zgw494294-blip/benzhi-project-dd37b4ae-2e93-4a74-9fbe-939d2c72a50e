package trialquerycachestale

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"seedvault/internal/httpui"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

func TestTrialQueryCacheRefreshesAfterMutation(t *testing.T) {
	data, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	service := workflow.New(data)
	source, err := service.CreateSource(workflow.SourceInput{
		ID: "src-query-cache", ScientificName: "珙桐", CollectionLocation: "四川卧龙",
		Collector: "实验员", CollectionDate: "2026-01-01", StorageCondition: "4C冷藏", LotCode: "QUERY-CACHE-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := service.CreateTrial(workflow.TrialInput{
		ID: "trial-query-cache", SourceID: source.ID, ProtocolName: "缓存失效验证协议",
		ReplicateCount: 1, TemperatureCelsius: 22, ObservationSchedule: []string{"第1天"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpui.New(service).Handler()

	first := queryTrials(t, handler, source.ID)
	if len(first.Items) != 1 || first.Items[0].Version != trial.Version {
		t.Fatalf("首次查询结果异常: %+v", first)
	}

	body, err := json.Marshal(map[string]any{
		"ExpectedVersion": trial.Version,
		"ObservedAt":      "第1天",
		"Observations": []workflow.ObservationInput{{
			ID: "obs-query-cache", ReplicateNo: 1, GerminatedCount: 3,
			TemperatureCelsius: 22, HumidityPercent: 60, EvidenceRef: "photo:query-cache",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/trials/"+trial.ID+"/observations", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("观测提交失败: status=%d body=%s", response.Code, response.Body.String())
	}

	updated, ok := data.Trial(trial.ID)
	if !ok || updated.Version != trial.Version+1 || len(updated.Observations) != 1 {
		t.Fatalf("Store 没有提交新版本: %+v", updated)
	}
	second := queryTrials(t, handler, source.ID)
	if len(second.Items) != 1 || second.Items[0].Version != updated.Version || len(second.Items[0].Observations) != 1 {
		t.Fatalf("TestTrialQueryCacheRefreshesAfterMutation: 写入后列表仍返回旧快照: got=%+v wantVersion=%d", second.Items, updated.Version)
	}
}

func queryTrials(t *testing.T, handler http.Handler, sourceID string) store.TrialPage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/trials?sourceID="+sourceID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("查询失败: status=%d body=%s", response.Code, response.Body.String())
	}
	var page store.TrialPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
