package session

import (
	"sync/atomic"
	"testing"
)

func newTestClient(bufSize int) *Client {
	return NewClient(nil, bufSize)
}

func TestDefaultFormat(t *testing.T) {
	c := newTestClient(10)
	if c.Format() != FormatJSON {
		t.Fatalf("default format = %d, want FormatJSON (%d)", c.Format(), FormatJSON)
	}
}

func TestSetFormat(t *testing.T) {
	c := newTestClient(10)
	c.SetFormat(FormatBinary)
	if c.Format() != FormatBinary {
		t.Fatalf("format = %d, want FormatBinary (%d)", c.Format(), FormatBinary)
	}
	c.SetFormat(FormatJSON)
	if c.Format() != FormatJSON {
		t.Fatalf("format = %d, want FormatJSON (%d)", c.Format(), FormatJSON)
	}
}

func TestSubscribe(t *testing.T) {
	c := newTestClient(10)
	c.Subscribe([]uint16{1, 5, 10})
	if !c.IsSubscribed(1) {
		t.Fatal("should be subscribed to locate 1")
	}
	if !c.IsSubscribed(5) {
		t.Fatal("should be subscribed to locate 5")
	}
	if c.IsSubscribed(2) {
		t.Fatal("should not be subscribed to locate 2")
	}
}

func TestSubscribeAll(t *testing.T) {
	c := newTestClient(10)
	c.SubscribeAll()
	if !c.IsSubscribed(1) {
		t.Fatal("should be subscribed to any locate after SubscribeAll")
	}
	if !c.IsSubscribed(999) {
		t.Fatal("should be subscribed to any locate after SubscribeAll")
	}
	if !c.IsAllSubscribed() {
		t.Fatal("IsAllSubscribed should be true")
	}
}

func TestUnsubscribe(t *testing.T) {
	c := newTestClient(10)
	c.Subscribe([]uint16{1, 5, 10})
	c.Unsubscribe([]uint16{5})
	if c.IsSubscribed(5) {
		t.Fatal("should not be subscribed to locate 5 after unsubscribe")
	}
	if !c.IsSubscribed(1) {
		t.Fatal("should still be subscribed to locate 1")
	}
}

func TestSubscribedLocates(t *testing.T) {
	c := newTestClient(10)
	c.Subscribe([]uint16{1, 5, 10})
	locs := c.SubscribedLocates()
	if len(locs) != 3 {
		t.Fatalf("SubscribedLocates returned %d, want 3", len(locs))
	}
	// Verify all subscribed locates are present
	locSet := make(map[uint16]bool)
	for _, l := range locs {
		locSet[l] = true
	}
	for _, want := range []uint16{1, 5, 10} {
		if !locSet[want] {
			t.Fatalf("locate %d missing from SubscribedLocates", want)
		}
	}
}

func TestSubscribedLocatesAllNil(t *testing.T) {
	c := newTestClient(10)
	c.SubscribeAll()
	locs := c.SubscribedLocates()
	if locs != nil {
		t.Fatalf("SubscribedLocates should return nil for all-subscribed, got %v", locs)
	}
}

func TestSendBufferFull(t *testing.T) {
	c := newTestClient(2) // buffer size 2
	ok1 := c.Send([]byte("msg1"))
	ok2 := c.Send([]byte("msg2"))
	ok3 := c.Send([]byte("msg3")) // should be dropped
	if !ok1 || !ok2 {
		t.Fatal("first two sends should succeed")
	}
	if ok3 {
		t.Fatal("third send should fail (buffer full)")
	}
	dropped := atomic.LoadUint64(&c.Dropped)
	if dropped != 1 {
		t.Fatalf("Dropped = %d, want 1", dropped)
	}
}

func TestSendNotFull(t *testing.T) {
	c := newTestClient(100)
	ok := c.Send([]byte("hello"))
	if !ok {
		t.Fatal("Send should succeed with large buffer")
	}
	dropped := atomic.LoadUint64(&c.Dropped)
	if dropped != 0 {
		t.Fatalf("Dropped = %d, want 0", dropped)
	}
}

func TestUniqueIDs(t *testing.T) {
	// Reset counter
	atomic.StoreUint64(&clientIDCounter, 0)
	c1 := newTestClient(10)
	c2 := newTestClient(10)
	c3 := newTestClient(10)
	if c1.ID == c2.ID || c2.ID == c3.ID || c1.ID == c3.ID {
		t.Fatalf("client IDs should be unique: %d, %d, %d", c1.ID, c2.ID, c3.ID)
	}
}

func TestIsSubscribedDefault(t *testing.T) {
	c := newTestClient(10)
	if c.IsSubscribed(1) {
		t.Fatal("new client should not be subscribed to any symbol")
	}
}
