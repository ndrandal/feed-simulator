package orderbook

import (
	"sync/atomic"
	"testing"
)

func TestSideConstants(t *testing.T) {
	if SideBuy != 'B' {
		t.Fatalf("SideBuy = %c, want B", SideBuy)
	}
	if SideSell != 'S' {
		t.Fatalf("SideSell = %c, want S", SideSell)
	}
}

func TestNextOrderIDMonotonic(t *testing.T) {
	// Reset counter for isolated test
	SetOrderIDCounter(0)
	prev := NextOrderID()
	for i := 0; i < 1000; i++ {
		cur := NextOrderID()
		if cur <= prev {
			t.Fatalf("NextOrderID not monotonic: %d <= %d", cur, prev)
		}
		prev = cur
	}
}

func TestNextMatchNumberMonotonic(t *testing.T) {
	// Reset counter for isolated test
	SetMatchCounter(0)
	prev := NextMatchNumber()
	for i := 0; i < 1000; i++ {
		cur := NextMatchNumber()
		if cur <= prev {
			t.Fatalf("NextMatchNumber not monotonic: %d <= %d", cur, prev)
		}
		prev = cur
	}
}

func TestSetGetOrderIDCounter(t *testing.T) {
	SetOrderIDCounter(12345)
	got := GetOrderIDCounter()
	if got != 12345 {
		t.Fatalf("GetOrderIDCounter = %d, want 12345", got)
	}
	// Cleanup
	atomic.StoreUint64(&orderIDCounter, 0)
}

func TestSetGetMatchCounter(t *testing.T) {
	SetMatchCounter(67890)
	got := GetMatchCounter()
	if got != 67890 {
		t.Fatalf("GetMatchCounter = %d, want 67890", got)
	}
	// Cleanup
	atomic.StoreUint64(&matchCounter, 0)
}

func TestOrderStruct(t *testing.T) {
	o := Order{
		ID:     1,
		Locate: 5,
		Side:   SideBuy,
		Price:  100.50,
		Shares: 500,
		MPID:   "GSCO",
	}
	if o.ID != 1 || o.Locate != 5 || o.Side != SideBuy || o.Price != 100.50 || o.Shares != 500 || o.MPID != "GSCO" {
		t.Fatal("Order struct fields not set correctly")
	}
}
