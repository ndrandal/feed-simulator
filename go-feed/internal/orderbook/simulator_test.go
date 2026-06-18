package orderbook

import (
	"testing"

	"github.com/ndrandal/feed-simulator/go-feed/internal/engine"
	"github.com/ndrandal/feed-simulator/go-feed/internal/itch"
)

func newTestSimulator() *Simulator {
	SetOrderIDCounter(0)
	SetMatchCounter(0)
	rng := engine.NewRNG(42)
	book := NewBook(1, 0.01)
	return NewSimulator(rng, book, 1, 0.01)
}

func TestInitializeMessageCount(t *testing.T) {
	sim := newTestSimulator()
	msgs := sim.Initialize(100.00)
	// MaxLevels=10, OrdersPerLevel=3, 2 sides = 10*3*2 = 60
	if len(msgs) != 60 {
		t.Fatalf("Initialize produced %d messages, want 60", len(msgs))
	}
}

func TestInitializeAllAddOrders(t *testing.T) {
	sim := newTestSimulator()
	msgs := sim.Initialize(100.00)
	for i, m := range msgs {
		if m.Type != itch.MsgAddOrder && m.Type != itch.MsgAddOrderMPID {
			t.Fatalf("msg[%d] type = %c, want A or F", i, m.Type)
		}
	}
}

func TestInitializeBidsAndAsks(t *testing.T) {
	sim := newTestSimulator()
	refPrice := 100.00
	sim.Initialize(refPrice)
	book := sim.Book()

	if book.BidLevels() == 0 {
		t.Fatal("no bid levels after Initialize")
	}
	if book.AskLevels() == 0 {
		t.Fatal("no ask levels after Initialize")
	}

	// All bids should be below refPrice
	bestBid := book.BestBid()
	if bestBid >= refPrice {
		t.Fatalf("BestBid %f >= refPrice %f", bestBid, refPrice)
	}

	// All asks should be above refPrice
	bestAsk := book.BestAsk()
	if bestAsk <= refPrice {
		t.Fatalf("BestAsk %f <= refPrice %f", bestAsk, refPrice)
	}
}

func TestInitializeBookPopulated(t *testing.T) {
	sim := newTestSimulator()
	sim.Initialize(100.00)
	book := sim.Book()
	if book.OrderCount() != 60 {
		t.Fatalf("OrderCount = %d, want 60", book.OrderCount())
	}
}

func TestInitializeSharesRoundLots(t *testing.T) {
	sim := newTestSimulator()
	msgs := sim.Initialize(100.00)
	for i, m := range msgs {
		if m.Shares%100 != 0 {
			t.Fatalf("msg[%d] shares = %d, not a round lot", i, m.Shares)
		}
		if m.Shares <= 0 {
			t.Fatalf("msg[%d] shares = %d, should be positive", i, m.Shares)
		}
	}
}

func TestInitializePriceSnapping(t *testing.T) {
	sim := newTestSimulator()
	msgs := sim.Initialize(100.00)
	for i, m := range msgs {
		cents := int64(m.Price * 100)
		reconstructed := float64(cents) / 100.0
		diff := m.Price - reconstructed
		if diff > 0.001 || diff < -0.001 {
			t.Fatalf("msg[%d] price %f not snapped to 0.01", i, m.Price)
		}
	}
}

func TestStepProducesMessages(t *testing.T) {
	sim := newTestSimulator()
	sim.Initialize(100.00)
	msgs := sim.Step(100.00, 3)
	if len(msgs) == 0 {
		t.Fatal("Step produced no messages")
	}
}

func TestStepValidTypes(t *testing.T) {
	sim := newTestSimulator()
	sim.Initialize(100.00)
	validTypes := map[itch.MsgType]bool{
		itch.MsgAddOrder:      true,
		itch.MsgAddOrderMPID:  true,
		itch.MsgOrderExecuted: true,
		itch.MsgOrderCancel:   true,
		itch.MsgOrderDelete:   true,
		itch.MsgOrderReplace:  true,
		itch.MsgTrade:         true,
	}
	for i := 0; i < 100; i++ {
		msgs := sim.Step(100.00, 3)
		for _, m := range msgs {
			if !validTypes[m.Type] {
				t.Fatalf("Step produced invalid type: %c", m.Type)
			}
		}
	}
}

func TestTradeExecutedPairing(t *testing.T) {
	sim := newTestSimulator()
	sim.Initialize(100.00)
	// Run many steps and check that E and P come in pairs with same match number
	for i := 0; i < 500; i++ {
		msgs := sim.Step(100.00, 3)
		for j := 0; j < len(msgs); j++ {
			if msgs[j].Type == itch.MsgOrderExecuted {
				if j+1 >= len(msgs) || msgs[j+1].Type != itch.MsgTrade {
					t.Fatal("OrderExecuted not followed by Trade")
				}
				if msgs[j].MatchNumber != msgs[j+1].MatchNumber {
					t.Fatalf("match number mismatch: executed=%d trade=%d", msgs[j].MatchNumber, msgs[j+1].MatchNumber)
				}
			}
		}
	}
}

func TestDeterministicSimulation(t *testing.T) {
	run := func() []itch.Message {
		SetOrderIDCounter(0)
		SetMatchCounter(0)
		rng := engine.NewRNG(42)
		book := NewBook(1, 0.01)
		sim := NewSimulator(rng, book, 1, 0.01)
		all := sim.Initialize(100.00)
		for i := 0; i < 50; i++ {
			all = append(all, sim.Step(100.00, 2)...)
		}
		return all
	}

	msgs1 := run()
	msgs2 := run()

	if len(msgs1) != len(msgs2) {
		t.Fatalf("determinism: different message counts %d vs %d", len(msgs1), len(msgs2))
	}
	for i := range msgs1 {
		if msgs1[i].Type != msgs2[i].Type || msgs1[i].Price != msgs2[i].Price || msgs1[i].Shares != msgs2[i].Shares {
			t.Fatalf("determinism: mismatch at message %d", i)
		}
	}
}

func TestBookAccessor(t *testing.T) {
	sim := newTestSimulator()
	book := sim.Book()
	if book == nil {
		t.Fatal("Book() returned nil")
	}
	if book.Locate != 1 {
		t.Fatalf("Book().Locate = %d, want 1", book.Locate)
	}
}

// TestSimulationNoOrphanedOrders drives a long randomized simulation and asserts
// the book never orphans orders: every order in orderMap must remain reachable
// through the visible price levels. Before the orderMap-eviction fix, level
// trimming stranded orders in orderMap and this invariant grew steadily violated
// (OrderCount >> reachable), which is the production OOM/restart leak.
func TestSimulationNoOrphanedOrders(t *testing.T) {
	sim := newTestSimulator()
	price := 100.00
	sim.Initialize(price)

	for i := 0; i < 5000; i++ {
		// Wander the price so levels churn and trims fire on both sides.
		price += float64((i%7)-3) * 0.01
		if price < 1.0 {
			price = 1.0
		}
		sim.Step(price, 3)
	}

	b := sim.Book()
	reachable := 0
	for _, lvl := range b.Depth().Bids {
		reachable += lvl.Orders
	}
	for _, lvl := range b.Depth().Asks {
		reachable += lvl.Orders
	}
	if b.OrderCount() != reachable {
		t.Fatalf("orphaned orders after simulation: OrderCount()=%d but only %d reachable",
			b.OrderCount(), reachable)
	}
}

// TestAddMsgsEvictionMessages verifies the wire semantics of an eviction: adding
// an order that displaces a previously-resting order emits an AddOrder for the
// new order followed by an OrderDelete for the displaced one; an order that is
// itself immediately trimmed emits nothing.
func TestAddMsgsEvictionMessages(t *testing.T) {
	sim := newTestSimulator()
	displaced := &Order{ID: 1001, Locate: 1, Side: SideBuy, Price: 90.00, Shares: 100}
	added := &Order{ID: 1002, Locate: 1, Side: SideBuy, Price: 100.00, Shares: 100}

	// Normal eviction: added displaces `displaced`.
	msgs := sim.addMsgs(added, []*Order{displaced})
	if len(msgs) != 2 {
		t.Fatalf("eviction produced %d msgs, want 2 (add+delete)", len(msgs))
	}
	if msgs[0].Type != itch.MsgAddOrder || msgs[0].OrderRef != added.ID {
		t.Fatalf("msg[0] = %c ref %d, want AddOrder for %d", msgs[0].Type, msgs[0].OrderRef, added.ID)
	}
	if msgs[1].Type != itch.MsgOrderDelete || msgs[1].OrderRef != displaced.ID {
		t.Fatalf("msg[1] = %c ref %d, want OrderDelete for %d", msgs[1].Type, msgs[1].OrderRef, displaced.ID)
	}

	// Self-eviction: the added order was itself trimmed off — no messages.
	msgs = sim.addMsgs(added, []*Order{added})
	if len(msgs) != 0 {
		t.Fatalf("self-eviction produced %d msgs, want 0", len(msgs))
	}
}
