package persist

import "testing"

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
