package persist

import (
	"testing"
	"time"
)

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{0, DefaultLimit},
		{-1, DefaultLimit},
		{1, 1},
		{100, 100},
		{MaxLimit, MaxLimit},
		{MaxLimit + 1, MaxLimit},
		{5000, MaxLimit},
	}
	for _, tt := range tests {
		if got := ClampLimit(tt.in); got != tt.want {
			t.Errorf("ClampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestValidInterval(t *testing.T) {
	for _, ok := range []string{"1m", "5m", "15m", "1h", "4h", "1d"} {
		if !ValidInterval(ok) {
			t.Errorf("ValidInterval(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "2m", "99x", "1M", "1week"} {
		if ValidInterval(bad) {
			t.Errorf("ValidInterval(%q) = true, want false", bad)
		}
	}
}

func TestAlignDown(t *testing.T) {
	// 2025-01-15T10:32:45Z, 1m bucket -> 10:32:00
	tm := time.Date(2025, 1, 15, 10, 32, 45, 0, time.UTC)
	if got := alignDown(tm, 60); !got.Equal(time.Date(2025, 1, 15, 10, 32, 0, 0, time.UTC)) {
		t.Errorf("alignDown 1m = %v", got)
	}
	// 5m bucket -> 10:30:00
	if got := alignDown(tm, 300); !got.Equal(time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)) {
		t.Errorf("alignDown 5m = %v", got)
	}
	// already aligned stays put
	aligned := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if got := alignDown(aligned, 300); !got.Equal(aligned) {
		t.Errorf("alignDown aligned = %v", got)
	}
}

func bucket(min int) time.Time {
	return time.Date(2025, 1, 15, 10, min, 0, 0, time.UTC)
}

func TestZeroFill(t *testing.T) {
	// DB has buckets at 10:30 and 10:27 (gaps at :29, :28). Fill 10:30..10:27.
	db := []Candle{
		{Bucket: bucket(30), Open: 1, High: 2, Low: 1, Close: 2, Volume: 100, Count: 5},
		{Bucket: bucket(27), Open: 3, High: 4, Low: 3, Close: 4, Volume: 50, Count: 2},
	}
	out := zeroFill(db, bucket(30), bucket(27), 60, 100)
	if len(out) != 4 {
		t.Fatalf("expected 4 contiguous buckets, got %d", len(out))
	}
	wantBuckets := []int{30, 29, 28, 27}
	for i, m := range wantBuckets {
		if !out[i].Bucket.Equal(bucket(m)) {
			t.Errorf("out[%d].Bucket = %v, want minute %d", i, out[i].Bucket, m)
		}
	}
	// filled gaps are zero-volume
	if out[1].Volume != 0 || out[1].Count != 0 || out[2].Volume != 0 {
		t.Errorf("expected zero bars at gaps, got %+v %+v", out[1], out[2])
	}
	// real bars preserved
	if out[0].Volume != 100 || out[3].Volume != 50 {
		t.Errorf("real bars not preserved: %+v %+v", out[0], out[3])
	}
}

func TestZeroFillRespectsLimit(t *testing.T) {
	out := zeroFill(nil, bucket(59), bucket(0), 60, 10)
	if len(out) != 10 {
		t.Fatalf("expected limit cap of 10, got %d", len(out))
	}
	// newest kept (10:59 first)
	if !out[0].Bucket.Equal(bucket(59)) {
		t.Errorf("expected newest bucket first, got %v", out[0].Bucket)
	}
}

func TestFillBounds(t *testing.T) {
	db := []Candle{{Bucket: bucket(30)}, {Bucket: bucket(20)}}

	// from/to take precedence and align
	f := CandleFilter{From: ptr(bucket(10)), To: ptr(time.Date(2025, 1, 15, 10, 40, 30, 0, time.UTC))}
	hi, lo, ok := f.fillBounds(60, db)
	if !ok || !hi.Equal(bucket(40)) || !lo.Equal(bucket(10)) {
		t.Errorf("from/to bounds: hi=%v lo=%v ok=%v", hi, lo, ok)
	}

	// before -> hi is bucket just under the cursor
	f = CandleFilter{Before: ptr(bucket(25))}
	hi, _, ok = f.fillBounds(60, db)
	if !ok || !hi.Equal(bucket(24)) {
		t.Errorf("before bound: hi=%v ok=%v", hi, ok)
	}

	// no bounds, no candles -> not ok
	if _, _, ok := (CandleFilter{}).fillBounds(60, nil); ok {
		t.Error("expected ok=false with no bounds and no candles")
	}
}

func ptr[T any](v T) *T { return &v }
