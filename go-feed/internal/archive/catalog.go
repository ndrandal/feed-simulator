package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fileExt is the suffix of an archived day-file. Day-files live at
// <dir>/trades/YYYY/MM/DD.jsonl.gz (the layout written by Archiver.writeBatch).
const fileExt = ".jsonl.gz"

// dayLayout is the YYYY/MM/DD path stem of a day-file, relative to <dir>/trades.
const dayLayout = "2006/01/02"

// DayFile is one archived UTC day of trades.
type DayFile struct {
	Date time.Time // UTC midnight of the archived day
	Path string    // absolute path to the .jsonl.gz file
}

// Catalog enumerates and resolves archived trade day-files under an archive
// directory. It only stats/walks the directory tree — it never decodes files —
// so it stays cheap and bounded.
type Catalog struct {
	dir string // archive root (ARCHIVE_DIR); empty means archiving is disabled
}

// NewCatalog returns a Catalog rooted at dir (ARCHIVE_DIR). An empty dir is
// valid and yields an empty catalog (archiving disabled).
func NewCatalog(dir string) *Catalog {
	return &Catalog{dir: dir}
}

// dayOf truncates t to UTC midnight.
func dayOf(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// Days returns every archived day-file in ascending date order. A disabled
// (empty) or missing archive directory yields an empty slice and no error.
func (c *Catalog) Days() ([]DayFile, error) {
	if c.dir == "" {
		return nil, nil
	}
	root := filepath.Join(c.dir, "trades")

	var days []DayFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Missing root (archiving never ran) is not an error.
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, fileExt) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		stem := strings.TrimSuffix(filepath.ToSlash(rel), fileExt)
		date, parseErr := time.Parse(dayLayout, stem)
		if parseErr != nil {
			// Ignore files that don't match the YYYY/MM/DD layout.
			return nil
		}
		days = append(days, DayFile{Date: date.UTC(), Path: path})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan archive: %w", err)
	}

	sort.Slice(days, func(i, j int) bool { return days[i].Date.Before(days[j].Date) })
	return days, nil
}

// Bounds returns the earliest and latest archived day. ok is false when the
// catalog is empty.
func (c *Catalog) Bounds() (min, max time.Time, ok bool, err error) {
	days, err := c.Days()
	if err != nil || len(days) == 0 {
		return time.Time{}, time.Time{}, false, err
	}
	return days[0].Date, days[len(days)-1].Date, true, nil
}

// Resolve returns the ordered day-files whose UTC day overlaps the inclusive
// [from, to] range. A day-file for day D covers [D, D+24h); it is included when
// its day falls within [dayOf(from), dayOf(to)]. If from is after to the range
// is treated as empty.
func (c *Catalog) Resolve(from, to time.Time) ([]DayFile, error) {
	if from.After(to) {
		return nil, nil
	}
	days, err := c.Days()
	if err != nil {
		return nil, err
	}
	lo, hi := dayOf(from), dayOf(to)

	out := make([]DayFile, 0, len(days))
	for _, df := range days {
		if df.Date.Before(lo) || df.Date.After(hi) {
			continue
		}
		out = append(out, df)
	}
	return out, nil
}

// Enabled reports whether an archive directory is configured.
func (c *Catalog) Enabled() bool { return c.dir != "" }
