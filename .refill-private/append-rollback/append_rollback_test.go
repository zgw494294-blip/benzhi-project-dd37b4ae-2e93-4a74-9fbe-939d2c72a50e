package appendrollback_test

import (
	"os"
	"path/filepath"
	"testing"

	"seedvault/internal/domain"
	"seedvault/internal/store"
)

func TestFailedAppendDoesNotPublishSource(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "events.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	source, err := domain.NewSeedSource("source-1", "Davidia involucrata", "四川卧龙", "实验员", "2026-01-01", "4C干燥", "LOT-001")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSource(source, 0); err == nil {
		t.Fatal("预期事件日志写入失败")
	}
	if _, ok := s.Source(source.ID); ok {
		t.Fatal("持久化失败后种源仍被发布到内存投影")
	}
	if got := s.EventCount(); got != 0 {
		t.Fatalf("持久化失败后事件计数为%d，期望0", got)
	}
}
