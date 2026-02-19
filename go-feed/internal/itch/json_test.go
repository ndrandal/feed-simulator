package itch

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeJSON(t *testing.T, m *Message) map[string]any {
	t.Helper()
	data, err := EncodeJSON(m)
	if err != nil {
		t.Fatalf("EncodeJSON error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	return obj
}

func TestEncodeJSONSystemEvent(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgSystemEvent, StockLocate: 0, Timestamp: 1000, EventCode: 'O'})
	if obj["type"] != "system_event" {
		t.Fatalf("type = %v, want system_event", obj["type"])
	}
	if obj["eventCode"] != "O" {
		t.Fatalf("eventCode = %v, want O", obj["eventCode"])
	}
}

func TestEncodeJSONStockDirectory(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgStockDirectory, StockLocate: 1, Stock: "NEXO", RoundLotSize: 100, MarketCategory: 'Q', FinancialStatus: 'N'})
	if obj["type"] != "stock_directory" {
		t.Fatalf("type = %v, want stock_directory", obj["type"])
	}
	if obj["stock"] != "NEXO" {
		t.Fatalf("stock = %v, want NEXO", obj["stock"])
	}
}

func TestEncodeJSONStockTradingAction(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgStockTradingAction, StockLocate: 1, Stock: "NEXO", TradingState: 'T'})
	if obj["type"] != "stock_trading_action" {
		t.Fatalf("type = %v, want stock_trading_action", obj["type"])
	}
}

func TestEncodeJSONAddOrder(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgAddOrder, StockLocate: 1, OrderRef: 42, Side: 'B', Shares: 500, Price: 125.50})
	if obj["type"] != "add_order" {
		t.Fatalf("type = %v, want add_order", obj["type"])
	}
	price, ok := obj["price"].(string)
	if !ok {
		t.Fatal("price should be a string")
	}
	if price != "125.5000" {
		t.Fatalf("price = %s, want 125.5000", price)
	}
}

func TestEncodeJSONAddOrderMPID(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgAddOrderMPID, StockLocate: 1, OrderRef: 42, Side: 'S', Shares: 300, Price: 99.99, MPID: "GSCO"})
	if obj["type"] != "add_order_mpid" {
		t.Fatalf("type = %v, want add_order_mpid", obj["type"])
	}
	if obj["mpid"] != "GSCO" {
		t.Fatalf("mpid = %v, want GSCO", obj["mpid"])
	}
}

func TestEncodeJSONOrderExecuted(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgOrderExecuted, StockLocate: 1, OrderRef: 42, Shares: 200, MatchNumber: 7})
	if obj["type"] != "order_executed" {
		t.Fatalf("type = %v, want order_executed", obj["type"])
	}
	if obj["matchNumber"] == nil {
		t.Fatal("matchNumber should be present")
	}
}

func TestEncodeJSONOrderCancel(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgOrderCancel, StockLocate: 1, OrderRef: 42, Shares: 100})
	if obj["type"] != "order_cancel" {
		t.Fatalf("type = %v, want order_cancel", obj["type"])
	}
}

func TestEncodeJSONOrderDelete(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgOrderDelete, StockLocate: 1, OrderRef: 42})
	if obj["type"] != "order_delete" {
		t.Fatalf("type = %v, want order_delete", obj["type"])
	}
}

func TestEncodeJSONOrderReplace(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgOrderReplace, StockLocate: 1, OrigOrderRef: 42, OrderRef: 43, Shares: 300, Price: 50.25})
	if obj["type"] != "order_replace" {
		t.Fatalf("type = %v, want order_replace", obj["type"])
	}
	price, ok := obj["price"].(string)
	if !ok {
		t.Fatal("price should be a string")
	}
	if price != "50.2500" {
		t.Fatalf("price = %s, want 50.2500", price)
	}
}

func TestEncodeJSONTrade(t *testing.T) {
	obj := decodeJSON(t, &Message{Type: MsgTrade, StockLocate: 1, OrderRef: 42, Side: 'B', Shares: 500, Stock: "NEXO", Price: 125.50, MatchNumber: 7})
	if obj["type"] != "trade" {
		t.Fatalf("type = %v, want trade", obj["type"])
	}
	if obj["matchNumber"] == nil {
		t.Fatal("matchNumber should be present")
	}
}

func TestEncodeJSONUnsupportedType(t *testing.T) {
	_, err := EncodeJSON(&Message{Type: MsgType('Z')})
	if err == nil {
		t.Fatal("expected error for unsupported message type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error should mention 'unsupported', got: %v", err)
	}
}

func TestEncodeJSONPriceFormat(t *testing.T) {
	// Verify prices are formatted as 4-decimal strings
	obj := decodeJSON(t, &Message{Type: MsgAddOrder, StockLocate: 1, OrderRef: 1, Side: 'B', Shares: 100, Price: 1.0})
	price := obj["price"].(string)
	if price != "1.0000" {
		t.Fatalf("price = %s, want 1.0000", price)
	}
}
