package engine

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// RNG is a seedable pseudo-random number generator using PCG-XSH-RR.
// It is safe for concurrent use.
type RNG struct {
	mu    sync.Mutex
	state uint64
	inc   uint64
	// spare gaussian value (Box-Muller)
	hasSpare bool
	spare    float64
}

// NewRNG creates a new PRNG with the given seed. If seed is 0, uses current time.
func NewRNG(seed int64) *RNG {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	r := &RNG{}
	// PCG requires odd increment
	r.inc = uint64(seed)<<1 | 1
	r.state = 0
	r.step()
	r.state += uint64(seed)
	r.step()
	return r
}

func (r *RNG) step() {
	r.state = r.state*6364136223846793005 + r.inc
}

// Uint32 returns a uniformly distributed uint32.
func (r *RNG) Uint32() uint32 {
	r.mu.Lock()
	old := r.state
	r.step()
	r.mu.Unlock()

	xorshifted := uint32(((old >> 18) ^ old) >> 27)
	rot := uint32(old >> 59)
	return (xorshifted >> rot) | (xorshifted << ((-rot) & 31))
}

// Uint64 returns a uniformly distributed uint64.
func (r *RNG) Uint64() uint64 {
	hi := uint64(r.Uint32())
	lo := uint64(r.Uint32())
	return hi<<32 | lo
}

// Float64 returns a uniformly distributed float64 in [0, 1).
func (r *RNG) Float64() float64 {
	return float64(r.Uint32()) / (1 << 32)
}

// Intn returns a uniformly distributed int in [0, n).
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Uint32() % uint32(n))
}

// IntRange returns a uniformly distributed int in [min, max].
func (r *RNG) IntRange(min, max int) int {
	if min >= max {
		return min
	}
	return min + r.Intn(max-min+1)
}

// Gaussian returns a standard normal random variable using Box-Muller.
func (r *RNG) Gaussian() float64 {
	r.mu.Lock()
	if r.hasSpare {
		r.hasSpare = false
		v := r.spare
		r.mu.Unlock()
		return v
	}
	r.mu.Unlock()

	var u, v, s float64
	for {
		u = r.Float64()*2 - 1
		v = r.Float64()*2 - 1
		s = u*u + v*v
		if s > 0 && s < 1 {
			break
		}
	}

	s = math.Sqrt(-2 * math.Log(s) / s)

	r.mu.Lock()
	r.spare = v * s
	r.hasSpare = true
	r.mu.Unlock()

	return u * s
}

// WeightedPick selects an index from weights using a weighted random choice.
func (r *RNG) WeightedPick(weights []float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	target := r.Float64() * total
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if target < cumulative {
			return i
		}
	}
	return len(weights) - 1
}

// State returns the internal PRNG state for persistence.
func (r *RNG) State() (state, inc uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.inc
}

// RestoreState sets the internal PRNG state from persisted values.
func (r *RNG) RestoreState(state, inc uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
	r.inc = inc
	r.hasSpare = false
}

// StateBytes returns the PRNG state as a byte slice for storage.
func (r *RNG) StateBytes() []byte {
	st, inc := r.State()
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], st)
	binary.BigEndian.PutUint64(buf[8:16], inc)
	return buf
}

// RestoreStateBytes restores PRNG state from a byte slice.
func (r *RNG) RestoreStateBytes(b []byte) {
	if len(b) < 16 {
		return
	}
	st := binary.BigEndian.Uint64(b[0:8])
	inc := binary.BigEndian.Uint64(b[8:16])
	r.RestoreState(st, inc)
}
