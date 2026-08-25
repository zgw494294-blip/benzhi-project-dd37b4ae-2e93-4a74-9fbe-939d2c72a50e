package store

import (
	"path/filepath"
	"seedvault/internal/domain"
	"testing"
)

func TestHashChainReplayAndConflict(t *testing.T) {
	d := t.TempDir()
	s, e := Open(filepath.Join(d, "db"))
	if e != nil {
		t.Fatal(e)
	}
	v, _ := domain.NewSeedSource("s", "学名", "地点", "采集人", "2026-01-01", "4C", "L01")
	if e = s.PutSource(v, 0); e != nil {
		t.Fatal(e)
	}
	if e = s.PutSource(v, 0); e != domain.ErrConflict {
		t.Fatalf("期望冲突，得到 %v", e)
	}
	r, e := Open(filepath.Join(d, "db"))
	if e != nil {
		t.Fatal(e)
	}
	got, ok := r.Source("s")
	if !ok || got.LotCode != "L01" {
		t.Fatalf("重放失败: %+v", got)
	}
}
