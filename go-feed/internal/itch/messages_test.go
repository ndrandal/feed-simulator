package itch

import "testing"

func TestPrice4RoundTrip(t *testing.T) {
	prices := []float64{0.0, 1.0, 99.99, 125.50, 0.0001}
	for _, p := range prices {
		got := Price4ToFloat(Price4(p))
		diff := got - p
		if diff < -0.00005 || diff > 0.00005 {
			t.Errorf("Price4 round-trip failed for %f: got %f", p, got)
		}
	}
}

func TestPrice4KnownValues(t *testing.T) {
	cases := []struct {
		price float64
		want  uint32
	}{
		{125.50, 1255000},
		{0.01, 100},
		{1.0, 10000},
	}
	for _, c := range cases {
		got := Price4(c.price)
		if got != c.want {
			t.Errorf("Price4(%f) = %d, want %d", c.price, got, c.want)
		}
	}
}

func TestPadStock(t *testing.T) {
	b := PadStock("AAPL")
	got := string(b[:])
	want := "AAPL    "
	if got != want {
		t.Errorf("PadStock(AAPL) = %q, want %q", got, want)
	}
}

func TestPadMPID(t *testing.T) {
	b := PadMPID("GS")
	got := string(b[:])
	want := "GS  "
	if got != want {
		t.Errorf("PadMPID(GS) = %q, want %q", got, want)
	}
}

func TestMsgTypeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  MsgType
		want byte
	}{
		{"SystemEvent", MsgSystemEvent, 'S'},
		{"StockDirectory", MsgStockDirectory, 'R'},
		{"StockTradingAction", MsgStockTradingAction, 'H'},
		{"AddOrder", MsgAddOrder, 'A'},
		{"AddOrderMPID", MsgAddOrderMPID, 'F'},
		{"OrderExecuted", MsgOrderExecuted, 'E'},
		{"OrderCancel", MsgOrderCancel, 'X'},
		{"OrderDelete", MsgOrderDelete, 'D'},
		{"OrderReplace", MsgOrderReplace, 'U'},
		{"Trade", MsgTrade, 'P'},
	}
	for _, c := range cases {
		if byte(c.got) != c.want {
			t.Errorf("%s: got %c, want %c", c.name, c.got, c.want)
		}
	}
}

func TestEventCodes(t *testing.T) {
	codes := []byte{
		EventStartOfMessages,
		EventStartOfSystem,
		EventStartOfMarket,
		EventEndOfMarket,
		EventEndOfSystem,
		EventEndOfMessages,
	}
	expected := []byte{'O', 'S', 'Q', 'M', 'E', 'C'}
	for i, c := range codes {
		if c != expected[i] {
			t.Errorf("event code[%d] = %c, want %c", i, c, expected[i])
		}
	}
}

func TestNanosFromMidnightRange(t *testing.T) {
	ns := NanosFromMidnight()
	// Must be non-negative and less than 24 hours in nanoseconds
	maxNanos := int64(24 * 60 * 60 * 1e9)
	if ns < 0 || ns >= maxNanos {
		t.Errorf("NanosFromMidnight() = %d, out of range [0, %d)", ns, maxNanos)
	}
}
