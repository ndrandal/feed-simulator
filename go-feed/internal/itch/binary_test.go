package itch

import (
	"encoding/binary"
	"testing"
)

func TestEncodeBinarySystemEvent(t *testing.T) {
	m := &Message{Type: MsgSystemEvent, StockLocate: 0, Timestamp: 123456, EventCode: EventStartOfMessages}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for SystemEvent")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 12 {
		t.Fatalf("SystemEvent body length = %d, want 12", bodyLen)
	}
	if data[2] != byte(MsgSystemEvent) {
		t.Fatalf("type byte = %c, want %c", data[2], MsgSystemEvent)
	}
	if data[len(data)-1] != EventStartOfMessages {
		t.Fatalf("event code = %c, want %c", data[len(data)-1], EventStartOfMessages)
	}
}

func TestEncodeBinaryStockDirectory(t *testing.T) {
	m := &Message{Type: MsgStockDirectory, StockLocate: 1, Stock: "NEXO", RoundLotSize: 100}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for StockDirectory")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 39 {
		t.Fatalf("StockDirectory body length = %d, want 39", bodyLen)
	}
	// Stock at offset 11 in body (13 in frame)
	stock := string(data[13:21])
	if stock != "NEXO    " {
		t.Fatalf("stock = %q, want %q", stock, "NEXO    ")
	}
}

func TestEncodeBinaryStockTradingAction(t *testing.T) {
	m := &Message{Type: MsgStockTradingAction, StockLocate: 1, Stock: "NEXO", TradingState: TradingResumed}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for StockTradingAction")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 25 {
		t.Fatalf("StockTradingAction body length = %d, want 25", bodyLen)
	}
}

func TestEncodeBinaryAddOrder(t *testing.T) {
	m := &Message{Type: MsgAddOrder, StockLocate: 1, OrderRef: 100, Side: 'B', Shares: 500, Price: 125.50}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for AddOrder")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 36 {
		t.Fatalf("AddOrder body length = %d, want 36", bodyLen)
	}
	// Price at body offset 32 (frame offset 34)
	priceRaw := binary.BigEndian.Uint32(data[34:38])
	if priceRaw != 1255000 {
		t.Fatalf("price = %d, want 1255000", priceRaw)
	}
}

func TestEncodeBinaryAddOrderMPID(t *testing.T) {
	m := &Message{Type: MsgAddOrderMPID, StockLocate: 1, OrderRef: 100, Side: 'B', Shares: 500, Price: 125.50, MPID: "GSCO"}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for AddOrderMPID")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 40 {
		t.Fatalf("AddOrderMPID body length = %d, want 40", bodyLen)
	}
	// MPID at body offset 36 (frame offset 38)
	mpid := string(data[38:42])
	if mpid != "GSCO" {
		t.Fatalf("MPID = %q, want %q", mpid, "GSCO")
	}
}

func TestEncodeBinaryOrderExecuted(t *testing.T) {
	m := &Message{Type: MsgOrderExecuted, StockLocate: 1, OrderRef: 100, Shares: 200, MatchNumber: 42}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for OrderExecuted")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 31 {
		t.Fatalf("OrderExecuted body length = %d, want 31", bodyLen)
	}
}

func TestEncodeBinaryOrderCancel(t *testing.T) {
	m := &Message{Type: MsgOrderCancel, StockLocate: 1, OrderRef: 100, Shares: 50}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for OrderCancel")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 23 {
		t.Fatalf("OrderCancel body length = %d, want 23", bodyLen)
	}
}

func TestEncodeBinaryOrderDelete(t *testing.T) {
	m := &Message{Type: MsgOrderDelete, StockLocate: 1, OrderRef: 100}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for OrderDelete")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 19 {
		t.Fatalf("OrderDelete body length = %d, want 19", bodyLen)
	}
}

func TestEncodeBinaryOrderReplace(t *testing.T) {
	m := &Message{Type: MsgOrderReplace, StockLocate: 1, OrigOrderRef: 100, OrderRef: 101, Shares: 300, Price: 50.25}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for OrderReplace")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 35 {
		t.Fatalf("OrderReplace body length = %d, want 35", bodyLen)
	}
	// OrigOrderRef at body offset 5+6=11 -> frame offset 13
	origRef := binary.BigEndian.Uint64(data[13:21])
	if origRef != 100 {
		t.Fatalf("origOrderRef = %d, want 100", origRef)
	}
}

func TestEncodeBinaryTrade(t *testing.T) {
	m := &Message{Type: MsgTrade, StockLocate: 1, OrderRef: 100, Side: 'B', Shares: 500, Stock: "NEXO", Price: 125.50, MatchNumber: 42}
	data := EncodeBinary(m)
	if data == nil {
		t.Fatal("EncodeBinary returned nil for Trade")
	}
	bodyLen := binary.BigEndian.Uint16(data[0:2])
	if bodyLen != 44 {
		t.Fatalf("Trade body length = %d, want 44", bodyLen)
	}
}

func TestEncodeBinaryUnknownType(t *testing.T) {
	m := &Message{Type: MsgType('Z')}
	data := EncodeBinary(m)
	if data != nil {
		t.Fatal("expected nil for unknown message type")
	}
}

func TestTimestamp6ByteEncoding(t *testing.T) {
	// Encode a message with a known timestamp and verify the 6-byte field
	ts := int64(0x0102030405_06)
	m := &Message{Type: MsgSystemEvent, Timestamp: ts, EventCode: 'O'}
	data := EncodeBinary(m)
	// Timestamp is at body offset 5 (frame offset 7), 6 bytes
	if data[7] != 0x01 || data[8] != 0x02 || data[9] != 0x03 ||
		data[10] != 0x04 || data[11] != 0x05 || data[12] != 0x06 {
		t.Errorf("timestamp bytes = %x, want 010203040506", data[7:13])
	}
}
