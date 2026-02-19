package engine

import (
	"math"
	"time"
)

// StressPhase represents the current intensity phase for the stress symbol.
type StressPhase int

const (
	PhaseCalm   StressPhase = 0
	PhaseActive StressPhase = 1
	PhaseBurst  StressPhase = 2
)

func (p StressPhase) String() string {
	switch p {
	case PhaseCalm:
		return "calm"
	case PhaseActive:
		return "active"
	case PhaseBurst:
		return "burst"
	default:
		return "unknown"
	}
}

// StressConfig holds the timing parameters for each phase.
type StressConfig struct {
	CalmMinMs  int
	CalmMaxMs  int
	ActiveMinMs int
	ActiveMaxMs int
	BurstMinMs  int
	BurstMaxMs  int
}

// DefaultStressConfig returns the default stress timing parameters.
func DefaultStressConfig() StressConfig {
	return StressConfig{
		CalmMinMs:   10,
		CalmMaxMs:   50,
		ActiveMinMs: 2,
		ActiveMaxMs: 10,
		BurstMinMs:  1,
		BurstMaxMs:  2,
	}
}

// StressController manages the variable-rate tick logic for BLITZ.
// It uses a sine-wave + random walk pattern for smooth phase transitions.
type StressController struct {
	rng    *RNG
	config StressConfig

	// Internal state
	phase        StressPhase
	phaseStart   time.Time
	phaseDuration time.Duration
	intensity    float64 // 0.0 (calm) to 1.0 (max burst)

	// Sine wave parameters
	t          float64 // time parameter for sine wave
	tStep      float64 // increment per call
	randomWalk float64 // additive random component
}

// NewStressController creates a new stress controller.
func NewStressController(rng *RNG, cfg StressConfig) *StressController {
	sc := &StressController{
		rng:        rng,
		config:     cfg,
		phase:      PhaseCalm,
		phaseStart: time.Now(),
		tStep:      0.01,
	}
	sc.phaseDuration = sc.randomDuration(30, 120) // calm lasts 30-120s
	return sc
}

// Tick advances the stress controller and returns the current tick interval
// and number of order book actions to perform.
func (sc *StressController) Tick() (interval time.Duration, numActions int) {
	// Update intensity using sine wave + random walk
	sc.t += sc.tStep
	sineComponent := (math.Sin(sc.t) + 1) / 2 // [0, 1]

	// Random walk with mean reversion
	sc.randomWalk += sc.rng.Gaussian() * 0.02
	sc.randomWalk *= 0.98 // mean revert

	sc.intensity = sineComponent + sc.randomWalk
	if sc.intensity < 0 {
		sc.intensity = 0
	}
	if sc.intensity > 1 {
		sc.intensity = 1
	}

	// Check for mega-spike (rare short burst of maximum throughput)
	if sc.rng.Float64() < 0.001 { // ~0.1% chance per tick
		sc.intensity = 1.0
	}

	// Determine phase from intensity
	now := time.Now()
	elapsed := now.Sub(sc.phaseStart)

	if elapsed >= sc.phaseDuration {
		// Phase transition
		sc.phaseStart = now
		sc.updatePhase()
	}

	// Calculate interval and actions based on phase + intensity
	switch sc.phase {
	case PhaseCalm:
		minMs := float64(sc.config.CalmMinMs)
		maxMs := float64(sc.config.CalmMaxMs)
		ms := maxMs - (maxMs-minMs)*sc.intensity
		interval = time.Duration(ms) * time.Millisecond
		numActions = 1 + int(sc.intensity*1) // 1-2

	case PhaseActive:
		minMs := float64(sc.config.ActiveMinMs)
		maxMs := float64(sc.config.ActiveMaxMs)
		ms := maxMs - (maxMs-minMs)*sc.intensity
		interval = time.Duration(ms) * time.Millisecond
		numActions = 3 + int(sc.intensity*2) // 3-5

	case PhaseBurst:
		minMs := float64(sc.config.BurstMinMs)
		maxMs := float64(sc.config.BurstMaxMs)
		ms := maxMs - (maxMs-minMs)*sc.intensity
		interval = time.Duration(ms) * time.Millisecond
		numActions = 5 + int(sc.intensity*5) // 5-10
	}

	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	return interval, numActions
}

// Phase returns the current stress phase.
func (sc *StressController) Phase() StressPhase {
	return sc.phase
}

// Intensity returns the current intensity level [0, 1].
func (sc *StressController) Intensity() float64 {
	return sc.intensity
}

func (sc *StressController) updatePhase() {
	if sc.intensity < 0.3 {
		sc.phase = PhaseCalm
		sc.phaseDuration = sc.randomDuration(30, 120)
	} else if sc.intensity < 0.7 {
		sc.phase = PhaseActive
		sc.phaseDuration = sc.randomDuration(10, 60)
	} else {
		sc.phase = PhaseBurst
		sc.phaseDuration = sc.randomDuration(5, 30)
	}
}

func (sc *StressController) randomDuration(minSec, maxSec int) time.Duration {
	secs := sc.rng.IntRange(minSec, maxSec)
	return time.Duration(secs) * time.Second
}
