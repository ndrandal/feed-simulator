package itch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON encoder — human-readable mirror of ITCH binary messages.
// Prices are formatted as 4-decimal strings, timestamps as int64 nanos.

// EncodeJSON encodes a Message into JSON bytes.
func EncodeJSON(m *Message) ([]byte, error) {
	obj := msgToMap(m)
	if obj == nil {
		return nil, fmt.Errorf("unsupported message type: %c", m.Type)
	}
	return json.Marshal(obj)
}

func msgToMap(m *Message) map[string]any {
	switch m.Type {
	case MsgSystemEvent:
		return map[string]any{
			"type":        "system_event",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"eventCode":   string([]byte{m.EventCode}),
		}

	case MsgStockDirectory:
		return map[string]any{
			"type":             "stock_directory",
			"timestamp":        m.Timestamp,
			"stockLocate":      m.StockLocate,
			"stock":            strings.TrimSpace(m.Stock),
			"marketCategory":   string([]byte{m.MarketCategory}),
			"financialStatus":  string([]byte{m.FinancialStatus}),
			"roundLotSize":     m.RoundLotSize,
			"roundLotsOnly":    string([]byte{m.RoundLotsOnly}),
		}

	case MsgStockTradingAction:
		return map[string]any{
			"type":         "stock_trading_action",
			"timestamp":    m.Timestamp,
			"stockLocate":  m.StockLocate,
			"stock":        strings.TrimSpace(m.Stock),
			"tradingState": string([]byte{m.TradingState}),
		}

	case MsgAddOrder:
		return map[string]any{
			"type":        "add_order",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"stock":       strings.TrimSpace(m.Stock),
			"orderRef":    m.OrderRef,
			"side":        string([]byte{m.Side}),
			"shares":      m.Shares,
			"price":       formatPrice(m.Price),
		}

	case MsgAddOrderMPID:
		return map[string]any{
			"type":        "add_order_mpid",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"stock":       strings.TrimSpace(m.Stock),
			"orderRef":    m.OrderRef,
			"side":        string([]byte{m.Side}),
			"shares":      m.Shares,
			"price":       formatPrice(m.Price),
			"mpid":        strings.TrimSpace(m.MPID),
		}

	case MsgOrderExecuted:
		return map[string]any{
			"type":        "order_executed",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"orderRef":    m.OrderRef,
			"shares":      m.Shares,
			"matchNumber": m.MatchNumber,
		}

	case MsgOrderCancel:
		return map[string]any{
			"type":        "order_cancel",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"orderRef":    m.OrderRef,
			"shares":      m.Shares,
		}

	case MsgOrderDelete:
		return map[string]any{
			"type":        "order_delete",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"orderRef":    m.OrderRef,
		}

	case MsgOrderReplace:
		return map[string]any{
			"type":         "order_replace",
			"timestamp":    m.Timestamp,
			"stockLocate":  m.StockLocate,
			"origOrderRef": m.OrigOrderRef,
			"orderRef":     m.OrderRef,
			"shares":       m.Shares,
			"price":        formatPrice(m.Price),
		}

	case MsgTrade:
		return map[string]any{
			"type":        "trade",
			"timestamp":   m.Timestamp,
			"stockLocate": m.StockLocate,
			"orderRef":    m.OrderRef,
			"side":        string([]byte{m.Side}),
			"shares":      m.Shares,
			"stock":       strings.TrimSpace(m.Stock),
			"price":       formatPrice(m.Price),
			"matchNumber": m.MatchNumber,
		}
	}
	return nil
}

func formatPrice(price float64) string {
	return fmt.Sprintf("%.4f", price)
}
