package itch

import (
	"encoding/binary"
)

// Binary ITCH 5.0 encoder.
// Each message is prefixed with a 2-byte length (SoupBinTCP-style framing).

// EncodeBinary encodes a Message into ITCH 5.0 binary format.
// Returns the encoded bytes including the 2-byte length prefix.
func EncodeBinary(m *Message) []byte {
	var body []byte

	switch m.Type {
	case MsgSystemEvent:
		body = encodeSystemEvent(m)
	case MsgStockDirectory:
		body = encodeStockDirectory(m)
	case MsgStockTradingAction:
		body = encodeStockTradingAction(m)
	case MsgAddOrder:
		body = encodeAddOrder(m)
	case MsgAddOrderMPID:
		body = encodeAddOrderMPID(m)
	case MsgOrderExecuted:
		body = encodeOrderExecuted(m)
	case MsgOrderCancel:
		body = encodeOrderCancel(m)
	case MsgOrderDelete:
		body = encodeOrderDelete(m)
	case MsgOrderReplace:
		body = encodeOrderReplace(m)
	case MsgTrade:
		body = encodeTrade(m)
	default:
		return nil
	}

	// 2-byte length prefix + body
	frame := make([]byte, 2+len(body))
	binary.BigEndian.PutUint16(frame[0:2], uint16(len(body)))
	copy(frame[2:], body)
	return frame
}

// putTimestamp writes a 6-byte nanosecond timestamp.
func putTimestamp(buf []byte, nanos int64) {
	// 6 bytes big-endian
	buf[0] = byte(nanos >> 40)
	buf[1] = byte(nanos >> 32)
	buf[2] = byte(nanos >> 24)
	buf[3] = byte(nanos >> 16)
	buf[4] = byte(nanos >> 8)
	buf[5] = byte(nanos)
}

// System Event Message (12 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + EventCode(1)
func encodeSystemEvent(m *Message) []byte {
	buf := make([]byte, 12)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	buf[11] = m.EventCode
	return buf
}

// Stock Directory Message (39 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + Stock(8) +
// MarketCategory(1) + FinancialStatus(1) + RoundLotSize(4) + RoundLotsOnly(1) +
// IssueClassification(1) + IssueSubType(2) + Authenticity(1) +
// ShortSaleThreshold(1) + IPOFlag(1) + LULDRefPriceTier(1) +
// ETPFlag(1) + ETPLeverageFactor(4) + InverseIndicator(1)
func encodeStockDirectory(m *Message) []byte {
	buf := make([]byte, 39)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	stock := PadStock(m.Stock)
	copy(buf[11:19], stock[:])
	buf[19] = m.MarketCategory
	buf[20] = m.FinancialStatus
	binary.BigEndian.PutUint32(buf[21:25], uint32(m.RoundLotSize))
	buf[25] = m.RoundLotsOnly
	buf[26] = m.IssueClassification
	copy(buf[27:29], m.IssueSubType[:])
	buf[29] = m.Authenticity
	buf[30] = m.ShortSaleThreshold
	buf[31] = m.IPOFlag
	buf[32] = m.LULDRefPriceTier
	buf[33] = m.ETPFlag
	binary.BigEndian.PutUint32(buf[34:38], uint32(m.ETPLeverageFactor))
	buf[38] = m.InverseIndicator
	return buf
}

// Stock Trading Action (25 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + Stock(8) +
// TradingState(1) + Reserved(1) + Reason(4)
func encodeStockTradingAction(m *Message) []byte {
	buf := make([]byte, 25)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	stock := PadStock(m.Stock)
	copy(buf[11:19], stock[:])
	buf[19] = m.TradingState
	buf[20] = m.Reserved
	// Reason: 4 bytes, space-padded
	copy(buf[21:25], "    ")
	return buf
}

// Add Order - No MPID (36 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrderRef(8) +
// Side(1) + Shares(4) + Stock(8) + Price(4)
func encodeAddOrder(m *Message) []byte {
	buf := make([]byte, 36)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	buf[19] = m.Side
	binary.BigEndian.PutUint32(buf[20:24], uint32(m.Shares))
	stock := PadStock(m.Stock)
	copy(buf[24:32], stock[:])
	binary.BigEndian.PutUint32(buf[32:36], Price4(m.Price))
	return buf
}

// Add Order with MPID (40 bytes)
// Same as Add Order + MPID(4)
func encodeAddOrderMPID(m *Message) []byte {
	buf := make([]byte, 40)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	buf[19] = m.Side
	binary.BigEndian.PutUint32(buf[20:24], uint32(m.Shares))
	stock := PadStock(m.Stock)
	copy(buf[24:32], stock[:])
	binary.BigEndian.PutUint32(buf[32:36], Price4(m.Price))
	mpid := PadMPID(m.MPID)
	copy(buf[36:40], mpid[:])
	return buf
}

// Order Executed (31 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrderRef(8) +
// Shares(4) + MatchNumber(8)
func encodeOrderExecuted(m *Message) []byte {
	buf := make([]byte, 31)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	binary.BigEndian.PutUint32(buf[19:23], uint32(m.Shares))
	binary.BigEndian.PutUint64(buf[23:31], m.MatchNumber)
	return buf
}

// Order Cancel (23 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrderRef(8) +
// CancelledShares(4)
func encodeOrderCancel(m *Message) []byte {
	buf := make([]byte, 23)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	binary.BigEndian.PutUint32(buf[19:23], uint32(m.Shares))
	return buf
}

// Order Delete (19 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrderRef(8)
func encodeOrderDelete(m *Message) []byte {
	buf := make([]byte, 19)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	return buf
}

// Order Replace (35 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrigOrderRef(8) +
// NewOrderRef(8) + Shares(4) + Price(4)
func encodeOrderReplace(m *Message) []byte {
	buf := make([]byte, 35)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrigOrderRef)
	binary.BigEndian.PutUint64(buf[19:27], m.OrderRef)
	binary.BigEndian.PutUint32(buf[27:31], uint32(m.Shares))
	binary.BigEndian.PutUint32(buf[31:35], Price4(m.Price))
	return buf
}

// Trade (Non-Cross) (44 bytes)
// Type(1) + StockLocate(2) + TrackingNum(2) + Timestamp(6) + OrderRef(8) +
// Side(1) + Shares(4) + Stock(8) + Price(4) + MatchNumber(8)
func encodeTrade(m *Message) []byte {
	buf := make([]byte, 44)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], m.StockLocate)
	binary.BigEndian.PutUint16(buf[3:5], m.TrackingNum)
	putTimestamp(buf[5:11], m.Timestamp)
	binary.BigEndian.PutUint64(buf[11:19], m.OrderRef)
	buf[19] = m.Side
	binary.BigEndian.PutUint32(buf[20:24], uint32(m.Shares))
	stock := PadStock(m.Stock)
	copy(buf[24:32], stock[:])
	binary.BigEndian.PutUint32(buf[32:36], Price4(m.Price))
	binary.BigEndian.PutUint64(buf[36:44], m.MatchNumber)
	return buf
}
