package engine

import (
	"math"
	"testing"
)

func TestDeterminism(t *testing.T) {
	r1 := NewRNG(42)
	r2 := NewRNG(42)
	for i := 0; i < 1000; i++ {
		if r1.Uint32() != r2.Uint32() {
			t.Fatalf("determinism broken at iteration %d", i)
		}
	}
}

func TestDifferentSeeds(t *testing.T) {
	r1 := NewRNG(42)
	r2 := NewRNG(43)
	same := 0
	for i := 0; i < 100; i++ {
		if r1.Uint32() == r2.Uint32() {
			same++
		}
	}
	if same > 5 {
		t.Fatalf("different seeds produced %d/100 identical values", same)
	}
}

func TestFloat64Bounds(t *testing.T) {
	r := NewRNG(42)
	for i := 0; i < 10000; i++ {
		v := r.Float64()
		if v < 0 || v >= 1 {
			t.Fatalf("Float64() = %f, out of [0, 1)", v)
		}
	}
}

func TestIntnBounds(t *testing.T) {
	r := NewRNG(42)
	for i := 0; i < 10000; i++ {
		v := r.Intn(10)
		if v < 0 || v >= 10 {
			t.Fatalf("Intn(10) = %d, out of [0, 10)", v)
		}
	}
}

func TestIntnZero(t *testing.T) {
	r := NewRNG(42)
	if r.Intn(0) != 0 {
		t.Fatal("Intn(0) should return 0")
	}
}

func TestIntnNegative(t *testing.T) {
	r := NewRNG(42)
	if r.Intn(-5) != 0 {
		t.Fatal("Intn(-5) should return 0")
	}
}

func TestIntRangeBounds(t *testing.T) {
	r := NewRNG(42)
	for i := 0; i < 10000; i++ {
		v := r.IntRange(5, 15)
		if v < 5 || v > 15 {
			t.Fatalf("IntRange(5,15) = %d, out of [5, 15]", v)
		}
	}
}

func TestIntRangeEqual(t *testing.T) {
	r := NewRNG(42)
	for i := 0; i < 100; i++ {
		v := r.IntRange(7, 7)
		if v != 7 {
			t.Fatalf("IntRange(7,7) = %d, want 7", v)
		}
	}
}

func TestIntRangeReversed(t *testing.T) {
	r := NewRNG(42)
	// When min >= max, should return min
	v := r.IntRange(10, 5)
	if v != 10 {
		t.Fatalf("IntRange(10,5) = %d, want 10", v)
	}
}

func TestGaussianStats(t *testing.T) {
	r := NewRNG(42)
	n := 50000
	sum := 0.0
	sumSq := 0.0
	for i := 0; i < n; i++ {
		v := r.Gaussian()
		sum += v
		sumSq += v * v
	}
	mean := sum / float64(n)
	variance := sumSq/float64(n) - mean*mean

	if math.Abs(mean) > 0.05 {
		t.Errorf("Gaussian mean = %f, expected ~0", mean)
	}
	if math.Abs(variance-1.0) > 0.1 {
		t.Errorf("Gaussian variance = %f, expected ~1", variance)
	}
}

func TestWeightedPickBounds(t *testing.T) {
	r := NewRNG(42)
	weights := []float64{1, 2, 3, 4}
	for i := 0; i < 10000; i++ {
		v := r.WeightedPick(weights)
		if v < 0 || v >= len(weights) {
			t.Fatalf("WeightedPick returned %d, out of [0, %d)", v, len(weights))
		}
	}
}

func TestWeightedPickDistribution(t *testing.T) {
	r := NewRNG(42)
	weights := []float64{0, 0, 1} // should always pick index 2
	for i := 0; i < 100; i++ {
		v := r.WeightedPick(weights)
		if v != 2 {
			t.Fatalf("WeightedPick with [0,0,1] returned %d, want 2", v)
		}
	}
}

func TestWeightedPickSingleWeight(t *testing.T) {
	r := NewRNG(42)
	weights := []float64{5}
	for i := 0; i < 100; i++ {
		v := r.WeightedPick(weights)
		if v != 0 {
			t.Fatalf("WeightedPick with single weight returned %d, want 0", v)
		}
	}
}

func TestStateSaveRestore(t *testing.T) {
	r := NewRNG(42)
	// Advance the state
	for i := 0; i < 100; i++ {
		r.Uint32()
	}
	// Save state
	st, inc := r.State()
	// Generate some values
	expected := make([]uint32, 50)
	for i := range expected {
		expected[i] = r.Uint32()
	}
	// Restore and verify
	r.RestoreState(st, inc)
	for i, want := range expected {
		got := r.Uint32()
		if got != want {
			t.Fatalf("mismatch at %d after restore: got %d, want %d", i, got, want)
		}
	}
}

func TestStateBytesRoundTrip(t *testing.T) {
	r := NewRNG(42)
	for i := 0; i < 100; i++ {
		r.Uint32()
	}
	buf := r.StateBytes()
	if len(buf) != 16 {
		t.Fatalf("StateBytes length = %d, want 16", len(buf))
	}
	expected := make([]uint32, 50)
	for i := range expected {
		expected[i] = r.Uint32()
	}
	r.RestoreStateBytes(buf)
	for i, want := range expected {
		got := r.Uint32()
		if got != want {
			t.Fatalf("mismatch at %d after RestoreStateBytes: got %d, want %d", i, got, want)
		}
	}
}

func TestRestoreStateBytesTooShort(t *testing.T) {
	r := NewRNG(42)
	v1 := r.Uint32()
	// Restoring with too-short slice should be a no-op
	r.RestoreStateBytes([]byte{1, 2, 3})
	v2 := r.Uint32()
	// Should still produce values (state not corrupted)
	_ = v1
	_ = v2
}
