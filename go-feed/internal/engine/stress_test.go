package engine

import (
	"testing"
	"time"
)

func TestPhaseString(t *testing.T) {
	cases := []struct {
		phase StressPhase
		want  string
	}{
		{PhaseCalm, "calm"},
		{PhaseActive, "active"},
		{PhaseBurst, "burst"},
		{StressPhase(99), "unknown"},
	}
	for _, c := range cases {
		got := c.phase.String()
		if got != c.want {
			t.Errorf("StressPhase(%d).String() = %q, want %q", c.phase, got, c.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultStressConfig()
	if cfg.CalmMinMs != 10 || cfg.CalmMaxMs != 50 {
		t.Errorf("calm range = [%d, %d], want [10, 50]", cfg.CalmMinMs, cfg.CalmMaxMs)
	}
	if cfg.ActiveMinMs != 2 || cfg.ActiveMaxMs != 10 {
		t.Errorf("active range = [%d, %d], want [2, 10]", cfg.ActiveMinMs, cfg.ActiveMaxMs)
	}
	if cfg.BurstMinMs != 1 || cfg.BurstMaxMs != 2 {
		t.Errorf("burst range = [%d, %d], want [1, 2]", cfg.BurstMinMs, cfg.BurstMaxMs)
	}
}

func TestIntensityBounds(t *testing.T) {
	rng := NewRNG(42)
	sc := NewStressController(rng, DefaultStressConfig())
	for i := 0; i < 10000; i++ {
		sc.Tick()
		intensity := sc.Intensity()
		if intensity < 0 || intensity > 1 {
			t.Fatalf("intensity = %f at tick %d, out of [0, 1]", intensity, i)
		}
	}
}

func TestIntervalMinimum(t *testing.T) {
	rng := NewRNG(42)
	sc := NewStressController(rng, DefaultStressConfig())
	for i := 0; i < 10000; i++ {
		interval, _ := sc.Tick()
		if interval < time.Millisecond {
			t.Fatalf("interval = %v at tick %d, below 1ms minimum", interval, i)
		}
	}
}

func TestActionCountsByPhase(t *testing.T) {
	rng := NewRNG(42)
	sc := NewStressController(rng, DefaultStressConfig())
	for i := 0; i < 10000; i++ {
		_, numActions := sc.Tick()
		phase := sc.Phase()
		switch phase {
		case PhaseCalm:
			if numActions < 1 || numActions > 2 {
				t.Fatalf("calm phase actions = %d, want [1, 2]", numActions)
			}
		case PhaseActive:
			if numActions < 3 || numActions > 5 {
				t.Fatalf("active phase actions = %d, want [3, 5]", numActions)
			}
		case PhaseBurst:
			if numActions < 5 || numActions > 10 {
				t.Fatalf("burst phase actions = %d, want [5, 10]", numActions)
			}
		}
	}
}

func TestPhaseTransitions(t *testing.T) {
	rng := NewRNG(42)
	cfg := DefaultStressConfig()
	sc := NewStressController(rng, cfg)
	// Override phase duration to force transitions
	sc.phaseDuration = time.Nanosecond

	seen := make(map[StressPhase]bool)
	for i := 0; i < 100000; i++ {
		sc.Tick()
		seen[sc.Phase()] = true
		if len(seen) == 3 {
			return // saw all phases
		}
	}
	t.Errorf("expected all 3 phases, only saw %d", len(seen))
}

func TestNewControllerStartsCalm(t *testing.T) {
	rng := NewRNG(42)
	sc := NewStressController(rng, DefaultStressConfig())
	if sc.Phase() != PhaseCalm {
		t.Fatalf("initial phase = %s, want calm", sc.Phase())
	}
}
