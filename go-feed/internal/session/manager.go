package session

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/ndrandal/feed-simulator/go-feed/internal/itch"
	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

// Manager handles client registration, subscriptions, and message fan-out.
type Manager struct {
	mu         sync.RWMutex
	clients    map[uint64]*Client
	symbols    []symbol.Symbol
	byTicker   map[string]uint16 // ticker -> locate code
	bufferSize int
}

// NewManager creates a session manager.
func NewManager(syms []symbol.Symbol, bufferSize int) *Manager {
	byTicker := make(map[string]uint16, len(syms))
	for _, s := range syms {
		byTicker[s.Ticker] = s.LocateCode
	}
	return &Manager{
		clients:    make(map[uint64]*Client),
		symbols:    syms,
		byTicker:   byTicker,
		bufferSize: bufferSize,
	}
}

// Register adds a new client. Returns the client for further use.
func (m *Manager) Register(conn *websocket.Conn) *Client {
	c := NewClient(conn, m.bufferSize)

	m.mu.Lock()
	m.clients[c.ID] = c
	m.mu.Unlock()

	log.Printf("client %d connected (%s)", c.ID, conn.RemoteAddr())
	return c
}

// Unregister removes a client.
func (m *Manager) Unregister(c *Client) {
	m.mu.Lock()
	delete(m.clients, c.ID)
	m.mu.Unlock()

	c.Close()
	log.Printf("client %d disconnected", c.ID)
}

// ResolveTickers converts ticker strings to locate codes.
// Returns nil for "*" (all symbols).
func (m *Manager) ResolveTickers(tickers []string) (locates []uint16, all bool) {
	for _, t := range tickers {
		if t == "*" {
			return nil, true
		}
		if loc, ok := m.byTicker[t]; ok {
			locates = append(locates, loc)
		}
	}
	return locates, false
}

// Broadcast sends a batch of ITCH messages to all subscribed clients.
// Messages are encoded once per format and fanned out.
func (m *Manager) Broadcast(locate uint16, stock string, msgs []itch.Message) {
	if len(msgs) == 0 {
		return
	}

	// Stamp all messages with timestamp and stock
	ts := itch.NanosFromMidnight()
	for i := range msgs {
		msgs[i].Timestamp = ts
		if msgs[i].Stock == "" {
			msgs[i].Stock = stock
		}
	}

	// Pre-encode for each format (lazy, only if needed)
	var jsonEncoded [][]byte
	var binaryEncoded [][]byte
	var jsonOnce, binaryOnce sync.Once

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, c := range m.clients {
		if !c.IsSubscribed(locate) {
			continue
		}

		switch c.Format() {
		case FormatJSON:
			jsonOnce.Do(func() {
				jsonEncoded = encodeAllJSON(msgs)
			})
			for _, data := range jsonEncoded {
				if !c.Send(data) {
					// buffer full, message dropped
				}
			}

		case FormatBinary:
			binaryOnce.Do(func() {
				binaryEncoded = encodeAllBinary(msgs)
			})
			for _, data := range binaryEncoded {
				if !c.Send(data) {
					// buffer full, message dropped
				}
			}
		}
	}
}

// SendToClient sends messages directly to a specific client (e.g., stock directory on connect).
func (m *Manager) SendToClient(c *Client, msgs []itch.Message) {
	ts := itch.NanosFromMidnight()
	for i := range msgs {
		msgs[i].Timestamp = ts
	}

	switch c.Format() {
	case FormatJSON:
		for _, data := range encodeAllJSON(msgs) {
			c.Send(data)
		}
	case FormatBinary:
		for _, data := range encodeAllBinary(msgs) {
			c.Send(data)
		}
	}
}

// ClientCount returns the number of connected clients.
func (m *Manager) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// Symbols returns the symbol list.
func (m *Manager) Symbols() []symbol.Symbol {
	return m.symbols
}

func encodeAllJSON(msgs []itch.Message) [][]byte {
	out := make([][]byte, 0, len(msgs))
	for i := range msgs {
		data, err := itch.EncodeJSON(&msgs[i])
		if err != nil {
			continue
		}
		out = append(out, data)
	}
	return out
}

func encodeAllBinary(msgs []itch.Message) [][]byte {
	out := make([][]byte, 0, len(msgs))
	for i := range msgs {
		data := itch.EncodeBinary(&msgs[i])
		if data != nil {
			out = append(out, data)
		}
	}
	return out
}
