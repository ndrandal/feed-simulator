package session

import (
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// Format represents the client's preferred encoding format.
type Format int

const (
	FormatJSON   Format = 0
	FormatBinary Format = 1
)

// Client represents a connected WebSocket client.
type Client struct {
	ID   uint64
	Conn *websocket.Conn

	mu          sync.RWMutex
	format      Format
	symbols     map[uint16]bool // locate code -> subscribed
	allSymbols  bool            // subscribed to all symbols

	sendCh      chan []byte
	done        chan struct{}
	closeOnce   sync.Once
	bufferSize  int

	// stats
	Dropped uint64
}

var clientIDCounter uint64

// NewClient creates a new client wrapping a WebSocket connection.
func NewClient(conn *websocket.Conn, bufferSize int) *Client {
	c := &Client{
		ID:         atomic.AddUint64(&clientIDCounter, 1),
		Conn:       conn,
		format:     FormatJSON,
		symbols:    make(map[uint16]bool),
		sendCh:     make(chan []byte, bufferSize),
		done:       make(chan struct{}),
		bufferSize: bufferSize,
	}
	return c
}

// Format returns the client's current encoding format.
func (c *Client) Format() Format {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.format
}

// SetFormat sets the client's encoding format.
func (c *Client) SetFormat(f Format) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.format = f
}

// Subscribe adds symbols to the client's subscription.
func (c *Client) Subscribe(locates []uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, loc := range locates {
		c.symbols[loc] = true
	}
}

// SubscribeAll subscribes the client to all symbols.
func (c *Client) SubscribeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allSymbols = true
}

// Unsubscribe removes symbols from the client's subscription.
func (c *Client) Unsubscribe(locates []uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, loc := range locates {
		delete(c.symbols, loc)
	}
}

// IsSubscribed checks if the client is subscribed to a given symbol.
func (c *Client) IsSubscribed(locate uint16) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.allSymbols {
		return true
	}
	return c.symbols[locate]
}

// SubscribedLocates returns the set of subscribed locate codes.
func (c *Client) SubscribedLocates() []uint16 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.allSymbols {
		return nil // caller should treat nil as "all"
	}
	out := make([]uint16, 0, len(c.symbols))
	for loc := range c.symbols {
		out = append(out, loc)
	}
	return out
}

// IsAllSubscribed returns true if the client is subscribed to all symbols.
func (c *Client) IsAllSubscribed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allSymbols
}

// Send enqueues data to be sent to the client.
// Returns false if the buffer is full (message dropped).
func (c *Client) Send(data []byte) bool {
	select {
	case c.sendCh <- data:
		return true
	default:
		atomic.AddUint64(&c.Dropped, 1)
		return false
	}
}

// SendCh returns the send channel for the write pump.
func (c *Client) SendCh() <-chan []byte {
	return c.sendCh
}

// Done returns a channel that is closed when the client is disconnected.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close terminates the client connection.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.Conn.Close()
	})
}
