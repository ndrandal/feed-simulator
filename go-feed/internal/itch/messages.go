package itch

import "time"

// Message type codes matching ITCH 5.0.
type MsgType byte

const (
	MsgSystemEvent      MsgType = 'S'
	MsgStockDirectory   MsgType = 'R'
	MsgStockTradingAction MsgType = 'H'
	MsgAddOrder         MsgType = 'A'
	MsgAddOrderMPID     MsgType = 'F'
	MsgOrderExecuted    MsgType = 'E'
	MsgOrderCancel      MsgType = 'X'
	MsgOrderDelete      MsgType = 'D'
	MsgOrderReplace     MsgType = 'U'
	MsgTrade            MsgType = 'P'
)

// System event codes.
const (
	EventStartOfMessages  byte = 'O'
	EventStartOfSystem    byte = 'S'
	EventStartOfMarket    byte = 'Q'
	EventEndOfMarket      byte = 'M'
	EventEndOfSystem      byte = 'E'
	EventEndOfMessages    byte = 'C'
)

// Trading state codes.
const (
	TradingHalted   byte = 'H'
	TradingPaused   byte = 'P'
	TradingResumed  byte = 'T' // trading/quoting
)

// Message is the universal message struct used throughout the simulator.
// Not all fields are used for every message type.
type Message struct {
	Type         MsgType
	Timestamp    int64   // nanoseconds since midnight UTC
	StockLocate  uint16
	TrackingNum  uint16
	Stock        string  // 8-char ticker
	OrderRef     uint64
	OrigOrderRef uint64  // for replace messages
	Side         byte    // 'B' or 'S'
	Shares       int32
	Price        float64
	MatchNumber  uint64
	MPID         string  // 4-char market participant
	EventCode    byte    // for system events
	TradingState byte    // for trading action
	Reserved     byte

	// Stock Directory fields
	MarketCategory      byte
	FinancialStatus     byte
	RoundLotSize        int32
	RoundLotsOnly       byte
	IssueClassification byte
	IssueSubType        [2]byte
	Authenticity        byte
	ShortSaleThreshold  byte
	IPOFlag             byte
	LULDRefPriceTier    byte
	ETPFlag             byte
	ETPLeverageFactor   int32
	InverseIndicator    byte
}

// NanosFromMidnight returns the current nanoseconds since midnight UTC.
func NanosFromMidnight() int64 {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return now.Sub(midnight).Nanoseconds()
}

// Price4 converts a float64 price to ITCH 4-decimal fixed-point (uint32).
// e.g., 125.50 -> 1255000
func Price4(price float64) uint32 {
	return uint32(price * 10000)
}

// Price4ToFloat converts ITCH fixed-point back to float64.
func Price4ToFloat(p uint32) float64 {
	return float64(p) / 10000
}

// PadStock right-pads a ticker to 8 bytes with spaces.
func PadStock(ticker string) [8]byte {
	var b [8]byte
	copy(b[:], ticker)
	for i := len(ticker); i < 8; i++ {
		b[i] = ' '
	}
	return b
}

// PadMPID right-pads an MPID to 4 bytes with spaces.
func PadMPID(mpid string) [4]byte {
	var b [4]byte
	copy(b[:], mpid)
	for i := len(mpid); i < 4; i++ {
		b[i] = ' '
	}
	return b
}
