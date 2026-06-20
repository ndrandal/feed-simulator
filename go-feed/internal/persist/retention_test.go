package persist

import "testing"

func TestSizeMonitorHighWater(t *testing.T) {
	m := &sizeMonitor{}
	steps := []struct {
		pct         float64
		wantWarn    bool
		wantCleared bool
	}{
		{50, false, false}, // below
		{79.9, false, false},
		{80, true, false},  // crosses high-water -> WARN
		{85, false, false}, // still above -> latched, no repeat
		{90, false, false},
		{76, false, false}, // within hysteresis band (>=75) -> no clear yet
		{74, false, true},  // drops below 75 -> cleared
		{74, false, false}, // stays cleared
		{81, true, false},  // re-crosses -> WARN again
	}
	for i, s := range steps {
		warn, cleared := m.updateHighWater(s.pct)
		if warn != s.wantWarn || cleared != s.wantCleared {
			t.Errorf("step %d (pct=%.1f): warn=%v cleared=%v, want warn=%v cleared=%v",
				i, s.pct, warn, cleared, s.wantWarn, s.wantCleared)
		}
	}
}
