package event_log_rotation_loss_test

import (
	"os"
	"path/filepath"
	"testing"

	"seedvault/internal/domain"
	"seedvault/internal/store"
)

func TestEventLogRotationDoesNotLoseAcknowledgedWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.NewSeedSource("source-before-rotation", "Davidia involucrata", "四川卧龙", "实验员甲", "2026-01-01", "4C干燥", "ROT-001")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSource(first, 0); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dir, "events.jsonl")
	rotatedPath := filepath.Join(dir, "events.jsonl.1")
	if err := os.Rename(logPath, rotatedPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	second, err := domain.NewSeedSource("source-after-rotation", "Taxus chinensis", "云南高黎贡山", "实验员乙", "2026-01-02", "4C干燥", "ROT-002")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSource(second, 0); err != nil {
		t.Fatalf("轮转后的写入未成功返回: %v", err)
	}

	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Source(second.ID); !ok {
		t.Fatalf("轮转后已确认成功的写入在重启后丢失")
	}
}
