package orderbook

import (
	"sync/atomic"
)

// Side represents bid or ask.
type Side byte

const (
	SideBuy  Side = 'B'
	SideSell Side = 'S'
)

// Order represents a single limit order on the book.
type Order struct {
	ID       uint64
	Locate   uint16
	Side     Side
	Price    float64
	Shares   int32
	Priority int32 // time priority within a price level
	MPID     string // market participant ID, empty for anonymous
}

// global order ID counter
var orderIDCounter uint64

// NextOrderID returns a globally unique order reference number.
func NextOrderID() uint64 {
	return atomic.AddUint64(&orderIDCounter, 1)
}

// SetOrderIDCounter sets the counter (for restoring from persistence).
func SetOrderIDCounter(val uint64) {
	atomic.StoreUint64(&orderIDCounter, val)
}

// GetOrderIDCounter returns the current counter value for persistence.
func GetOrderIDCounter() uint64 {
	return atomic.LoadUint64(&orderIDCounter)
}

// global match number counter for trades
var matchCounter uint64

// NextMatchNumber returns a globally unique trade match number.
func NextMatchNumber() uint64 {
	return atomic.AddUint64(&matchCounter, 1)
}

// SetMatchCounter sets the match counter (for restoring from persistence).
func SetMatchCounter(val uint64) {
	atomic.StoreUint64(&matchCounter, val)
}

// GetMatchCounter returns the current match counter for persistence.
func GetMatchCounter() uint64 {
	return atomic.LoadUint64(&matchCounter)
}
