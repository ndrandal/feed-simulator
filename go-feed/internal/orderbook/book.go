package orderbook

import (
	"sort"
	"sync"
)

const (
	MaxLevels     = 10 // 10 bid levels, 10 ask levels
	OrdersPerLevel = 3  // initial orders per level
)

// PriceLevel holds orders at a single price point.
type PriceLevel struct {
	Price  float64
	Orders []*Order
}

// Book is a price-time priority order book for a single symbol.
type Book struct {
	mu       sync.RWMutex
	Locate   uint16
	TickSize float64
	Bids     []PriceLevel // sorted descending by price
	Asks     []PriceLevel // sorted ascending by price
	orderMap map[uint64]*Order // quick lookup by order ID
}

// NewBook creates an empty order book for a symbol.
func NewBook(locate uint16, tickSize float64) *Book {
	return &Book{
		Locate:   locate,
		TickSize: tickSize,
		orderMap: make(map[uint64]*Order),
	}
}

// MidPrice returns the midpoint between best bid and best ask.
// Returns 0 if either side is empty.
func (b *Book) MidPrice() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.midPriceUnlocked()
}

func (b *Book) midPriceUnlocked() float64 {
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		return 0
	}
	return (b.Bids[0].Price + b.Asks[0].Price) / 2
}

// BestBid returns the best bid price, or 0 if empty.
func (b *Book) BestBid() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.Bids) == 0 {
		return 0
	}
	return b.Bids[0].Price
}

// BestAsk returns the best ask price, or 0 if empty.
func (b *Book) BestAsk() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.Asks) == 0 {
		return 0
	}
	return b.Asks[0].Price
}

// AddOrder inserts an order into the book at the appropriate price level.
// Returns the order that was added.
func (b *Book) AddOrder(o *Order) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.orderMap[o.ID] = o

	if o.Side == SideBuy {
		b.Bids = addToSide(b.Bids, o, true)
	} else {
		b.Asks = addToSide(b.Asks, o, false)
	}
}

// RemoveOrder removes an order by ID. Returns the removed order or nil.
func (b *Book) RemoveOrder(orderID uint64) *Order {
	b.mu.Lock()
	defer b.mu.Unlock()

	o, ok := b.orderMap[orderID]
	if !ok {
		return nil
	}
	delete(b.orderMap, orderID)

	if o.Side == SideBuy {
		b.Bids = removeFromSide(b.Bids, orderID)
	} else {
		b.Asks = removeFromSide(b.Asks, orderID)
	}
	return o
}

// GetOrder returns an order by ID, or nil if not found.
func (b *Book) GetOrder(orderID uint64) *Order {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.orderMap[orderID]
}

// ReduceOrder reduces the shares on an order. Returns the remaining shares.
func (b *Book) ReduceOrder(orderID uint64, reduceBy int32) int32 {
	b.mu.Lock()
	defer b.mu.Unlock()

	o, ok := b.orderMap[orderID]
	if !ok {
		return 0
	}
	o.Shares -= reduceBy
	if o.Shares <= 0 {
		o.Shares = 0
		delete(b.orderMap, orderID)
		if o.Side == SideBuy {
			b.Bids = removeFromSide(b.Bids, orderID)
		} else {
			b.Asks = removeFromSide(b.Asks, orderID)
		}
	}
	return o.Shares
}

// ReplaceOrder replaces an order with a new price/size. Returns the new order.
func (b *Book) ReplaceOrder(oldID uint64, newPrice float64, newShares int32) *Order {
	b.mu.Lock()
	defer b.mu.Unlock()

	old, ok := b.orderMap[oldID]
	if !ok {
		return nil
	}

	// Remove old
	delete(b.orderMap, oldID)
	if old.Side == SideBuy {
		b.Bids = removeFromSide(b.Bids, oldID)
	} else {
		b.Asks = removeFromSide(b.Asks, oldID)
	}

	// Create replacement
	newOrder := &Order{
		ID:     NextOrderID(),
		Locate: old.Locate,
		Side:   old.Side,
		Price:  newPrice,
		Shares: newShares,
		MPID:   old.MPID,
	}
	b.orderMap[newOrder.ID] = newOrder

	if newOrder.Side == SideBuy {
		b.Bids = addToSide(b.Bids, newOrder, true)
	} else {
		b.Asks = addToSide(b.Asks, newOrder, false)
	}

	return newOrder
}

// AllOrders returns all orders in the book (for persistence).
func (b *Book) AllOrders() []*Order {
	b.mu.RLock()
	defer b.mu.RUnlock()
	orders := make([]*Order, 0, len(b.orderMap))
	for _, o := range b.orderMap {
		orders = append(orders, o)
	}
	return orders
}

// OrderCount returns the total number of orders in the book.
func (b *Book) OrderCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.orderMap)
}

// BidLevels returns the number of bid price levels.
func (b *Book) BidLevels() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.Bids)
}

// AskLevels returns the number of ask price levels.
func (b *Book) AskLevels() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.Asks)
}

// RandomBidOrder returns a random order from the bid side, or nil.
func (b *Book) RandomBidOrder(idx int) *Order {
	b.mu.RLock()
	defer b.mu.RUnlock()
	count := 0
	for _, lvl := range b.Bids {
		for _, o := range lvl.Orders {
			if count == idx {
				return o
			}
			count++
		}
	}
	return nil
}

// RandomAskOrder returns a random order from the ask side, or nil.
func (b *Book) RandomAskOrder(idx int) *Order {
	b.mu.RLock()
	defer b.mu.RUnlock()
	count := 0
	for _, lvl := range b.Asks {
		for _, o := range lvl.Orders {
			if count == idx {
				return o
			}
			count++
		}
	}
	return nil
}

// TotalBidOrders returns the total number of bid orders.
func (b *Book) TotalBidOrders() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, lvl := range b.Bids {
		n += len(lvl.Orders)
	}
	return n
}

// TotalAskOrders returns the total number of ask orders.
func (b *Book) TotalAskOrders() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, lvl := range b.Asks {
		n += len(lvl.Orders)
	}
	return n
}

// RestoreOrder adds an order to the book during state restoration.
// Same as AddOrder but without generating a new ID.
func (b *Book) RestoreOrder(o *Order) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.orderMap[o.ID] = o
	if o.Side == SideBuy {
		b.Bids = addToSide(b.Bids, o, true)
	} else {
		b.Asks = addToSide(b.Asks, o, false)
	}
}

// DepthLevel represents aggregated data at a single price level.
type DepthLevel struct {
	Price       float64
	Orders      int
	TotalShares int32
}

// DepthSnapshot is a point-in-time snapshot of the order book.
type DepthSnapshot struct {
	Bids     []DepthLevel
	Asks     []DepthLevel
	BestBid  float64
	BestAsk  float64
	MidPrice float64
	Spread   float64
}

// Depth returns a thread-safe snapshot of the book's bid/ask levels.
func (b *Book) Depth() DepthSnapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snap := DepthSnapshot{}

	for _, lvl := range b.Bids {
		var total int32
		for _, o := range lvl.Orders {
			total += o.Shares
		}
		snap.Bids = append(snap.Bids, DepthLevel{
			Price:       lvl.Price,
			Orders:      len(lvl.Orders),
			TotalShares: total,
		})
	}

	for _, lvl := range b.Asks {
		var total int32
		for _, o := range lvl.Orders {
			total += o.Shares
		}
		snap.Asks = append(snap.Asks, DepthLevel{
			Price:       lvl.Price,
			Orders:      len(lvl.Orders),
			TotalShares: total,
		})
	}

	if len(b.Bids) > 0 {
		snap.BestBid = b.Bids[0].Price
	}
	if len(b.Asks) > 0 {
		snap.BestAsk = b.Asks[0].Price
	}
	if snap.BestBid > 0 && snap.BestAsk > 0 {
		snap.MidPrice = (snap.BestBid + snap.BestAsk) / 2
		snap.Spread = snap.BestAsk - snap.BestBid
	}

	return snap
}

// --- helpers ---

func addToSide(levels []PriceLevel, o *Order, descending bool) []PriceLevel {
	// Find existing level
	for i := range levels {
		if levels[i].Price == o.Price {
			levels[i].Orders = append(levels[i].Orders, o)
			return levels
		}
	}

	// New level
	newLevel := PriceLevel{Price: o.Price, Orders: []*Order{o}}
	levels = append(levels, newLevel)

	// Sort
	if descending {
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price > levels[j].Price })
	} else {
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price < levels[j].Price })
	}

	// Trim to max levels
	if len(levels) > MaxLevels {
		levels = levels[:MaxLevels]
	}
	return levels
}

func removeFromSide(levels []PriceLevel, orderID uint64) []PriceLevel {
	for i := range levels {
		for j := range levels[i].Orders {
			if levels[i].Orders[j].ID == orderID {
				levels[i].Orders = append(levels[i].Orders[:j], levels[i].Orders[j+1:]...)
				if len(levels[i].Orders) == 0 {
					levels = append(levels[:i], levels[i+1:]...)
				}
				return levels
			}
		}
	}
	return levels
}
