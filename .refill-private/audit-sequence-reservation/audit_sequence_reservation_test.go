package audit_sequence_reservation_test

import (
	"fmt"
	"sync"
	"testing"

	"seedvault/internal/audit"
	"seedvault/internal/store"
)

func TestConcurrentAuditSequenceReservationIsAtomic(t *testing.T) {
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	primary := audit.New(st)
	secondary := audit.New(st)
	const aggregateID = "trial-concurrent-audit"
	if err := primary.Record(aggregateID, "trial.created", "实验员", "实验员", "", 0, 1, map[string]string{"phase": "initial"}); err != nil {
		t.Fatal(err)
	}
	if !primary.VerifyTrail(aggregateID).Valid || !secondary.VerifyTrail(aggregateID).Valid {
		t.Fatal("并发写入前的审计链无效")
	}

	const workers = 32
	start := make(chan struct{})
	errors := make(chan error, workers)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			service := primary
			if index%2 == 1 {
				service = secondary
			}
			errors <- service.Record(
				aggregateID,
				"observation.added",
				fmt.Sprintf("实验员-%d", index),
				"实验员",
				"",
				index+1,
				index+2,
				map[string]string{"worker": fmt.Sprint(index)},
			)
		}(worker)
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("并发审计写入失败: %v", err)
		}
	}

	verification := primary.VerifyTrail(aggregateID)
	if verification.EntryCount != workers+1 {
		t.Fatalf("审计条目数量错误: got %d want %d", verification.EntryCount, workers+1)
	}
	if !verification.Valid {
		t.Fatalf("并发审计写入破坏了序号与哈希链: %v", verification.Failures)
	}
}
