package audit_view_cache_race_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"seedvault/internal/audit"
	"seedvault/internal/store"
)

func TestConcurrentAuditViewCacheIsIsolated(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previousProcs)

	storage, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	service := audit.New(storage)

	const workers = 32
	for index := 0; index < workers; index++ {
		aggregateID := fmt.Sprintf("trial-%02d", index)
		if err := service.Record(aggregateID, "trial.created", "实验员", "实验员", "", 0, 1, map[string]string{"stage": "created"}); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errorsFound := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			aggregateID := fmt.Sprintf("trial-%02d", index)
			entries := service.Entries(aggregateID)
			if len(entries) != 1 || entries[0].Details["stage"] != "created" {
				errorsFound <- fmt.Errorf("%s 的审计读模型不完整", aggregateID)
			}
		}(index)
	}
	close(start)
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}

	first := service.Entries("trial-00")
	first[0].Action = "polluted"
	first[0].Details["stage"] = "polluted"
	second := service.Entries("trial-00")
	verification := service.VerifyTrail("trial-00")
	if second[0].Action != "trial.created" || second[0].Details["stage"] != "created" || !verification.Valid {
		t.Fatalf("调用方修改审计视图后污染了缓存并使校验失效: entry=%+v verification=%+v", second[0], verification)
	}
}
