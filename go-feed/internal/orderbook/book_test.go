package orderbook

import (
	"testing"
)

func TestEmptyBook(t *testing.T) {
	b := NewBook(1, 0.01)
	if b.MidPrice() != 0 {
		t.Fatal("empty book MidPrice should be 0")
	}
	if b.BestBid() != 0 {
		t.Fatal("empty book BestBid should be 0")
	}
	if b.BestAsk() != 0 {
		t.Fatal("empty book BestAsk should be 0")
	}
	if b.OrderCount() != 0 {
		t.Fatal("empty book OrderCount should be 0")
	}
}

func TestAddSingleBid(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	if b.BestBid() != 100.00 {
		t.Fatalf("BestBid = %f, want 100.00", b.BestBid())
	}
	if b.OrderCount() != 1 {
		t.Fatal("OrderCount should be 1")
	}
}

func TestAddSingleAsk(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideSell, Price: 101.00, Shares: 100})
	if b.BestAsk() != 101.00 {
		t.Fatalf("BestAsk = %f, want 101.00", b.BestAsk())
	}
}

func TestBidDescendingSorting(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 99.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 3, Side: SideBuy, Price: 98.00, Shares: 100})
	if b.BestBid() != 100.00 {
		t.Fatalf("BestBid = %f, want 100.00 (highest bid)", b.BestBid())
	}
	if b.BidLevels() != 3 {
		t.Fatalf("BidLevels = %d, want 3", b.BidLevels())
	}
}

func TestAskAscendingSorting(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideSell, Price: 102.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideSell, Price: 101.00, Shares: 100})
	b.AddOrder(&Order{ID: 3, Side: SideSell, Price: 103.00, Shares: 100})
	if b.BestAsk() != 101.00 {
		t.Fatalf("BestAsk = %f, want 101.00 (lowest ask)", b.BestAsk())
	}
	if b.AskLevels() != 3 {
		t.Fatalf("AskLevels = %d, want 3", b.AskLevels())
	}
}

func TestMidPrice(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideSell, Price: 102.00, Shares: 100})
	mid := b.MidPrice()
	if mid != 101.00 {
		t.Fatalf("MidPrice = %f, want 101.00", mid)
	}
}

func TestAddSameLevel(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideBuy, Price: 100.00, Shares: 200})
	if b.BidLevels() != 1 {
		t.Fatalf("expected 1 bid level, got %d", b.BidLevels())
	}
	if b.OrderCount() != 2 {
		t.Fatalf("expected 2 orders, got %d", b.OrderCount())
	}
}

func TestMaxLevelsTrimming(t *testing.T) {
	b := NewBook(1, 0.01)
	// Add more than MaxLevels bid levels
	for i := 0; i < MaxLevels+5; i++ {
		b.AddOrder(&Order{ID: uint64(i + 1), Side: SideBuy, Price: float64(100 - i), Shares: 100})
	}
	if b.BidLevels() > MaxLevels {
		t.Fatalf("bid levels = %d, should be capped at %d", b.BidLevels(), MaxLevels)
	}
}

func TestRemoveOrderExists(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	removed := b.RemoveOrder(1)
	if removed == nil {
		t.Fatal("RemoveOrder returned nil for existing order")
	}
	if b.OrderCount() != 0 {
		t.Fatal("OrderCount should be 0 after removal")
	}
}

func TestRemoveOrderNotExists(t *testing.T) {
	b := NewBook(1, 0.01)
	removed := b.RemoveOrder(999)
	if removed != nil {
		t.Fatal("RemoveOrder should return nil for non-existing order")
	}
}

func TestRemoveLastAtLevel(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.RemoveOrder(1)
	if b.BidLevels() != 0 {
		t.Fatalf("BidLevels = %d, want 0 after removing last order at level", b.BidLevels())
	}
}

func TestRemoveNotLastAtLevel(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideBuy, Price: 100.00, Shares: 200})
	b.RemoveOrder(1)
	if b.BidLevels() != 1 {
		t.Fatalf("BidLevels = %d, want 1 (one order remains at level)", b.BidLevels())
	}
	if b.OrderCount() != 1 {
		t.Fatalf("OrderCount = %d, want 1", b.OrderCount())
	}
}

func TestGetOrder(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 42, Side: SideBuy, Price: 100.00, Shares: 500})
	o := b.GetOrder(42)
	if o == nil {
		t.Fatal("GetOrder returned nil")
	}
	if o.Shares != 500 {
		t.Fatalf("GetOrder shares = %d, want 500", o.Shares)
	}
	if b.GetOrder(999) != nil {
		t.Fatal("GetOrder should return nil for missing ID")
	}
}

func TestReduceOrderPartial(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 500})
	remaining := b.ReduceOrder(1, 200)
	if remaining != 300 {
		t.Fatalf("ReduceOrder remaining = %d, want 300", remaining)
	}
	if b.OrderCount() != 1 {
		t.Fatal("order should still be in book after partial reduce")
	}
}

func TestReduceOrderFull(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 500})
	remaining := b.ReduceOrder(1, 500)
	if remaining != 0 {
		t.Fatalf("ReduceOrder remaining = %d, want 0", remaining)
	}
	if b.OrderCount() != 0 {
		t.Fatal("order should be removed after full reduce")
	}
}

func TestReduceOrderOver(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 500})
	remaining := b.ReduceOrder(1, 999)
	if remaining != 0 {
		t.Fatalf("ReduceOrder remaining = %d, want 0 (over-reduce)", remaining)
	}
	if b.OrderCount() != 0 {
		t.Fatal("order should be removed after over-reduce")
	}
}

func TestReplaceOrder(t *testing.T) {
	// Reset the order counter so we get predictable IDs
	SetOrderIDCounter(100)
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 50, Side: SideBuy, Price: 100.00, Shares: 500})
	newOrder := b.ReplaceOrder(50, 101.00, 300)
	if newOrder == nil {
		t.Fatal("ReplaceOrder returned nil")
	}
	if newOrder.Price != 101.00 || newOrder.Shares != 300 {
		t.Fatalf("replaced order: price=%f shares=%d, want 101.00/300", newOrder.Price, newOrder.Shares)
	}
	if b.GetOrder(50) != nil {
		t.Fatal("old order should be removed")
	}
	if b.GetOrder(newOrder.ID) == nil {
		t.Fatal("new order should be in book")
	}
	if b.OrderCount() != 1 {
		t.Fatalf("OrderCount = %d, want 1", b.OrderCount())
	}
}

func TestReplaceOrderMissing(t *testing.T) {
	b := NewBook(1, 0.01)
	result := b.ReplaceOrder(999, 100.00, 100)
	if result != nil {
		t.Fatal("ReplaceOrder should return nil for missing order")
	}
}

func TestAllOrders(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideSell, Price: 101.00, Shares: 200})
	orders := b.AllOrders()
	if len(orders) != 2 {
		t.Fatalf("AllOrders returned %d orders, want 2", len(orders))
	}
}

func TestRestoreOrder(t *testing.T) {
	b := NewBook(1, 0.01)
	o := &Order{ID: 42, Side: SideBuy, Price: 100.00, Shares: 500}
	b.RestoreOrder(o)
	got := b.GetOrder(42)
	if got == nil {
		t.Fatal("RestoreOrder: order not found in book")
	}
	if got.Shares != 500 {
		t.Fatalf("RestoreOrder: shares = %d, want 500", got.Shares)
	}
}

func TestDepthSnapshot(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideBuy, Price: 100.00, Shares: 200})
	b.AddOrder(&Order{ID: 3, Side: SideSell, Price: 101.00, Shares: 300})

	snap := b.Depth()
	if len(snap.Bids) != 1 {
		t.Fatalf("Depth bids = %d, want 1", len(snap.Bids))
	}
	if snap.Bids[0].TotalShares != 300 {
		t.Fatalf("Depth bid shares = %d, want 300", snap.Bids[0].TotalShares)
	}
	if snap.Bids[0].Orders != 2 {
		t.Fatalf("Depth bid orders = %d, want 2", snap.Bids[0].Orders)
	}
	if snap.BestBid != 100.00 {
		t.Fatalf("Depth BestBid = %f, want 100.00", snap.BestBid)
	}
	if snap.BestAsk != 101.00 {
		t.Fatalf("Depth BestAsk = %f, want 101.00", snap.BestAsk)
	}
	if snap.MidPrice != 100.50 {
		t.Fatalf("Depth MidPrice = %f, want 100.50", snap.MidPrice)
	}
	if snap.Spread != 1.00 {
		t.Fatalf("Depth Spread = %f, want 1.00", snap.Spread)
	}
}

func TestRandomBidOrder(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideBuy, Price: 100.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideBuy, Price: 99.00, Shares: 200})
	o := b.RandomBidOrder(0)
	if o == nil {
		t.Fatal("RandomBidOrder(0) returned nil")
	}
	if o.ID != 1 {
		t.Fatalf("RandomBidOrder(0) = order %d, want 1 (best bid)", o.ID)
	}
	// Out of range
	if b.RandomBidOrder(999) != nil {
		t.Fatal("RandomBidOrder(999) should return nil")
	}
}

func TestRandomAskOrder(t *testing.T) {
	b := NewBook(1, 0.01)
	b.AddOrder(&Order{ID: 1, Side: SideSell, Price: 101.00, Shares: 100})
	b.AddOrder(&Order{ID: 2, Side: SideSell, Price: 102.00, Shares: 200})
	o := b.RandomAskOrder(0)
	if o == nil {
		t.Fatal("RandomAskOrder(0) returned nil")
	}
	if o.ID != 1 {
		t.Fatalf("RandomAskOrder(0) = order %d, want 1 (best ask)", o.ID)
	}
	if b.RandomAskOrder(999) != nil {
		t.Fatal("RandomAskOrder(999) should return nil")
	}
}
