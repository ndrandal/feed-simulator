package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFixture creates empty day-files at <dir>/trades/<day>.jsonl.gz for each
// "YYYY/MM/DD" day given.
func writeFixture(t *testing.T, dir string, days ...string) {
	t.Helper()
	for _, day := range days {
		path := filepath.Join(dir, "trades", filepath.FromSlash(day)+fileExt)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestCatalogDays(t *testing.T) {
	dir := t.TempDir()
	// Intentionally out of order, plus noise files that must be ignored.
	writeFixture(t, dir, "2026/06/18", "2026/06/16", "2026/06/17")
	writeFixture(t, dir, "2026/06/20") // will add a stray non-matching file alongside
	if err := os.WriteFile(filepath.Join(dir, "trades", "2026", "06", "README.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewCatalog(dir)
	days, err := c.Days()
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	if len(days) != 4 {
		t.Fatalf("expected 4 day-files, got %d: %+v", len(days), days)
	}
	// Ascending order.
	want := []time.Time{day(2026, 6, 16), day(2026, 6, 17), day(2026, 6, 18), day(2026, 6, 20)}
	for i, w := range want {
		if !days[i].Date.Equal(w) {
			t.Errorf("days[%d] = %v, want %v", i, days[i].Date, w)
		}
		if days[i].Path == "" {
			t.Errorf("days[%d] has empty path", i)
		}
	}
}

func TestCatalogBounds(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "2026/06/16", "2026/06/20", "2026/06/18")

	c := NewCatalog(dir)
	min, max, ok, err := c.Bounds()
	if err != nil || !ok {
		t.Fatalf("Bounds: ok=%v err=%v", ok, err)
	}
	if !min.Equal(day(2026, 6, 16)) || !max.Equal(day(2026, 6, 20)) {
		t.Errorf("bounds = [%v, %v]", min, max)
	}
}

func TestCatalogResolve(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "2026/06/15", "2026/06/16", "2026/06/17", "2026/06/18", "2026/06/19")
	c := NewCatalog(dir)

	// Range mid-month, with sub-day times that should truncate to the day.
	from := time.Date(2026, 6, 16, 13, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 18, 2, 0, 0, 0, time.UTC)
	got, err := c.Resolve(from, to)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []time.Time{day(2026, 6, 16), day(2026, 6, 17), day(2026, 6, 18)}
	if len(got) != len(want) {
		t.Fatalf("expected %d files, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if !got[i].Date.Equal(w) {
			t.Errorf("resolve[%d] = %v, want %v", i, got[i].Date, w)
		}
	}

	// Inverted range -> empty.
	if got, _ := c.Resolve(to, from); len(got) != 0 {
		t.Errorf("expected empty for inverted range, got %d", len(got))
	}

	// Range entirely before the archive -> empty.
	if got, _ := c.Resolve(day(2026, 1, 1), day(2026, 1, 31)); len(got) != 0 {
		t.Errorf("expected empty for out-of-range, got %d", len(got))
	}
}

func TestCatalogDisabledAndMissing(t *testing.T) {
	// Empty dir (archiving disabled).
	if days, err := NewCatalog("").Days(); err != nil || len(days) != 0 {
		t.Errorf("disabled catalog: days=%d err=%v", len(days), err)
	}
	if NewCatalog("").Enabled() {
		t.Error("empty dir should report Enabled() == false")
	}

	// Configured dir that doesn't exist yet (archiver hasn't run).
	missing := filepath.Join(t.TempDir(), "nope")
	c := NewCatalog(missing)
	if !c.Enabled() {
		t.Error("configured dir should report Enabled() == true")
	}
	days, err := c.Days()
	if err != nil {
		t.Errorf("missing archive dir should not error, got %v", err)
	}
	if len(days) != 0 {
		t.Errorf("missing archive dir should yield 0 days, got %d", len(days))
	}
	if _, _, ok, _ := c.Bounds(); ok {
		t.Error("missing archive dir should have no bounds")
	}
}
