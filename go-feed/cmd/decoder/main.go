// Command decoder connects to the feed simulator WebSocket in binary mode,
// subscribes to symbols, and prints every ITCH message in human-readable form.
//
// Usage:
//
//	decoder                              # connect to localhost:8100, subscribe to all
//	decoder -url ws://host:8100/feed     # custom endpoint
//	decoder -symbols BLITZ,NEXO          # subscribe to specific symbols
//	decoder -json                        # request JSON format instead (pass-through print)
//	decoder -stats 10                    # print message rate stats every N seconds
//	decoder -hex                         # also dump raw hex alongside decoded output
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := flag.String("url", "ws://localhost:8100/feed", "WebSocket endpoint")
	symbols := flag.String("symbols", "*", "Comma-separated symbols or * for all")
	useJSON := flag.Bool("json", false, "Request JSON format instead of binary")
	statsInterval := flag.Int("stats", 0, "Print message rate stats every N seconds (0 = off)")
	showHex := flag.Bool("hex", false, "Print raw hex dump alongside decoded output")
	flag.Parse()

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// Connect
	log.Printf("connecting to %s", *url)
	conn, _, err := websocket.DefaultDialer.Dial(*url, nil)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	log.Println("connected")

	// Set format
	format := "binary"
	if *useJSON {
		format = "json"
	}
	sendControl(conn, map[string]any{"action": "format", "format": format})

	// Subscribe
	symList := strings.Split(*symbols, ",")
	sendControl(conn, map[string]any{"action": "subscribe", "symbols": symList})
	log.Printf("subscribed to %s in %s mode", *symbols, format)

	// Stats counter
	var msgCount uint64
	if *statsInterval > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(*statsInterval) * time.Second)
			defer ticker.Stop()
			var last uint64
			for range ticker.C {
				cur := atomic.LoadUint64(&msgCount)
				delta := cur - last
				rate := float64(delta) / float64(*statsInterval)
				log.Printf("[stats] %d msgs total | %.1f msgs/sec", cur, rate)
				last = cur
			}
		}()
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()

	// Read loop
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("read: %v", err)
		}

		atomic.AddUint64(&msgCount, 1)

		if msgType == websocket.TextMessage || *useJSON {
			// JSON pass-through
			fmt.Println(string(data))
			continue
		}

		// Binary ITCH frame(s)
		decodeBinaryFrames(data, *showHex)
	}
}

func sendControl(conn *websocket.Conn, msg map[string]any) {
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Fatalf("send control: %v", err)
	}
}

// decodeBinaryFrames parses one or more 2-byte-length-prefixed ITCH messages
// from a single WebSocket binary frame.
func decodeBinaryFrames(data []byte, showHex bool) {
	// A single WS frame may contain exactly one ITCH message (with length prefix)
	// or just the raw body. Handle both cases.
	if len(data) < 2 {
		fmt.Printf("??? short frame (%d bytes)\n", len(data))
		return
	}

	// Check if data starts with a valid 2-byte length prefix
	frameLen := int(binary.BigEndian.Uint16(data[0:2]))
	if frameLen+2 == len(data) {
		// Length-prefixed: strip prefix, decode body
		body := data[2:]
		if showHex {
			printHex(data)
		}
		decodeMessage(body)
		return
	}

	// Possibly multiple concatenated frames, or raw body without prefix
	offset := 0
	decoded := false
	for offset+2 < len(data) {
		frameLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if frameLen <= 0 || offset+2+frameLen > len(data) {
			break
		}
		body := data[offset+2 : offset+2+frameLen]
		if showHex {
			printHex(data[offset : offset+2+frameLen])
		}
		decodeMessage(body)
		offset += 2 + frameLen
		decoded = true
	}

	if !decoded {
		// Treat the whole frame as a raw message body (no length prefix)
		if showHex {
			printHex(data)
		}
		decodeMessage(data)
	}
}

func decodeMessage(body []byte) {
	if len(body) == 0 {
		return
	}

	msgType := body[0]
	switch msgType {
	case 'S':
		decodeSystemEvent(body)
	case 'R':
		decodeStockDirectory(body)
	case 'H':
		decodeStockTradingAction(body)
	case 'A':
		decodeAddOrder(body)
	case 'F':
		decodeAddOrderMPID(body)
	case 'E':
		decodeOrderExecuted(body)
	case 'X':
		decodeOrderCancel(body)
	case 'D':
		decodeOrderDelete(body)
	case 'U':
		decodeOrderReplace(body)
	case 'P':
		decodeTrade(body)
	default:
		fmt.Printf("UNKNOWN  type=%c (0x%02x) len=%d\n", msgType, msgType, len(body))
	}
}

// --- Timestamp helper ---

func readTimestamp(buf []byte) int64 {
	// 6 bytes big-endian nanoseconds since midnight
	return int64(buf[0])<<40 | int64(buf[1])<<32 | int64(buf[2])<<24 |
		int64(buf[3])<<16 | int64(buf[4])<<8 | int64(buf[5])
}

func fmtTimestamp(nanos int64) string {
	d := time.Duration(nanos) * time.Nanosecond
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	us := (nanos / 1000) % 1000000
	return fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, us)
}

func readStock(buf []byte) string {
	return strings.TrimRight(string(buf), " ")
}

func fmtPrice4(raw uint32) string {
	whole := raw / 10000
	frac := raw % 10000
	return fmt.Sprintf("%d.%04d", whole, frac)
}

func fmtSide(b byte) string {
	switch b {
	case 'B':
		return "BUY"
	case 'S':
		return "SELL"
	default:
		return string(b)
	}
}

// --- Decoders for each message type ---

// System Event: Type(1) + Locate(2) + Tracking(2) + Timestamp(6) + EventCode(1) = 12
func decodeSystemEvent(b []byte) {
	if len(b) < 12 {
		fmt.Printf("SYSTEM   truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	event := b[11]

	eventName := map[byte]string{
		'O': "START_MESSAGES", 'S': "START_SYSTEM", 'Q': "START_MARKET",
		'M': "END_MARKET", 'E': "END_SYSTEM", 'C': "END_MESSAGES",
	}
	name := eventName[event]
	if name == "" {
		name = fmt.Sprintf("0x%02x", event)
	}

	fmt.Printf("SYSTEM   %s  locate=%d  event=%s\n", fmtTimestamp(ts), locate, name)
}

// Stock Directory: 39 bytes
func decodeStockDirectory(b []byte) {
	if len(b) < 39 {
		fmt.Printf("STOCKDIR truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	stock := readStock(b[11:19])
	mktCat := b[19]
	finStatus := b[20]
	lotSize := binary.BigEndian.Uint32(b[21:25])

	fmt.Printf("STOCKDIR %s  locate=%-3d  stock=%-8s  mktCat=%c  finStatus=%c  lotSize=%d\n",
		fmtTimestamp(ts), locate, stock, mktCat, finStatus, lotSize)
}

// Stock Trading Action: 25 bytes
func decodeStockTradingAction(b []byte) {
	if len(b) < 25 {
		fmt.Printf("TRADING  truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	stock := readStock(b[11:19])
	state := b[19]

	stateName := map[byte]string{'H': "HALTED", 'P': "PAUSED", 'T': "TRADING"}
	name := stateName[state]
	if name == "" {
		name = string(state)
	}

	fmt.Printf("TRADING  %s  locate=%-3d  stock=%-8s  state=%s\n",
		fmtTimestamp(ts), locate, stock, name)
}

// Add Order (no MPID): 36 bytes
func decodeAddOrder(b []byte) {
	if len(b) < 36 {
		fmt.Printf("ADD      truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])
	side := b[19]
	shares := binary.BigEndian.Uint32(b[20:24])
	stock := readStock(b[24:32])
	price := binary.BigEndian.Uint32(b[32:36])

	fmt.Printf("ADD      %s  locate=%-3d  stock=%-8s  ref=%-10d  %4s  %5d @ %s\n",
		fmtTimestamp(ts), locate, stock, orderRef, fmtSide(side), shares, fmtPrice4(price))
}

// Add Order with MPID: 40 bytes
func decodeAddOrderMPID(b []byte) {
	if len(b) < 40 {
		fmt.Printf("ADD+MPID truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])
	side := b[19]
	shares := binary.BigEndian.Uint32(b[20:24])
	stock := readStock(b[24:32])
	price := binary.BigEndian.Uint32(b[32:36])
	mpid := readStock(b[36:40])

	fmt.Printf("ADD+MPID %s  locate=%-3d  stock=%-8s  ref=%-10d  %4s  %5d @ %s  mpid=%s\n",
		fmtTimestamp(ts), locate, stock, orderRef, fmtSide(side), shares, fmtPrice4(price), mpid)
}

// Order Executed: 31 bytes
func decodeOrderExecuted(b []byte) {
	if len(b) < 31 {
		fmt.Printf("EXEC     truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])
	shares := binary.BigEndian.Uint32(b[19:23])
	matchNum := binary.BigEndian.Uint64(b[23:31])

	fmt.Printf("EXEC     %s  locate=%-3d  ref=%-10d  shares=%5d  match=%d\n",
		fmtTimestamp(ts), locate, orderRef, shares, matchNum)
}

// Order Cancel: 23 bytes
func decodeOrderCancel(b []byte) {
	if len(b) < 23 {
		fmt.Printf("CANCEL   truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])
	shares := binary.BigEndian.Uint32(b[19:23])

	fmt.Printf("CANCEL   %s  locate=%-3d  ref=%-10d  cancelled=%d\n",
		fmtTimestamp(ts), locate, orderRef, shares)
}

// Order Delete: 19 bytes
func decodeOrderDelete(b []byte) {
	if len(b) < 19 {
		fmt.Printf("DELETE   truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])

	fmt.Printf("DELETE   %s  locate=%-3d  ref=%d\n",
		fmtTimestamp(ts), locate, orderRef)
}

// Order Replace: 35 bytes
func decodeOrderReplace(b []byte) {
	if len(b) < 35 {
		fmt.Printf("REPLACE  truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	origRef := binary.BigEndian.Uint64(b[11:19])
	newRef := binary.BigEndian.Uint64(b[19:27])
	shares := binary.BigEndian.Uint32(b[27:31])
	price := binary.BigEndian.Uint32(b[31:35])

	fmt.Printf("REPLACE  %s  locate=%-3d  orig=%-10d  new=%-10d  %5d @ %s\n",
		fmtTimestamp(ts), locate, origRef, newRef, shares, fmtPrice4(price))
}

// Trade (Non-Cross): 44 bytes
func decodeTrade(b []byte) {
	if len(b) < 44 {
		fmt.Printf("TRADE    truncated (%d bytes)\n", len(b))
		return
	}
	locate := binary.BigEndian.Uint16(b[1:3])
	ts := readTimestamp(b[5:11])
	orderRef := binary.BigEndian.Uint64(b[11:19])
	side := b[19]
	shares := binary.BigEndian.Uint32(b[20:24])
	stock := readStock(b[24:32])
	price := binary.BigEndian.Uint32(b[32:36])
	matchNum := binary.BigEndian.Uint64(b[36:44])

	fmt.Printf("TRADE    %s  locate=%-3d  stock=%-8s  ref=%-10d  %4s  %5d @ %s  match=%d\n",
		fmtTimestamp(ts), locate, stock, orderRef, fmtSide(side), shares, fmtPrice4(price), matchNum)
}

// --- Hex dump ---

func printHex(data []byte) {
	var sb strings.Builder
	sb.WriteString("         hex: ")
	for i, b := range data {
		if i > 0 && i%16 == 0 {
			sb.WriteString("\n              ")
		}
		fmt.Fprintf(&sb, "%02x ", b)
	}
	fmt.Println(sb.String())
}
