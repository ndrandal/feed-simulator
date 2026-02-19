package session

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ndrandal/feed-simulator/go-feed/internal/itch"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 4096
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// controlMessage represents a client → server control message.
type controlMessage struct {
	Action  string   `json:"action"`
	Symbols []string `json:"symbols,omitempty"`
	Format  string   `json:"format,omitempty"`
}

// Handler creates the HTTP handler for WebSocket upgrades.
func Handler(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}

		client := mgr.Register(conn)

		// Start read and write pumps
		go writePump(client)
		go readPump(client, mgr)
	}
}

// readPump processes incoming control messages from the client.
func readPump(c *Client, mgr *Manager) {
	defer mgr.Unregister(c)

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("client %d read error: %v", c.ID, err)
			}
			return
		}

		var ctrl controlMessage
		if err := json.Unmarshal(message, &ctrl); err != nil {
			log.Printf("client %d invalid message: %v", c.ID, err)
			continue
		}

		handleControl(c, mgr, &ctrl)
	}
}

// handleControl processes a parsed control message.
func handleControl(c *Client, mgr *Manager, ctrl *controlMessage) {
	switch ctrl.Action {
	case "subscribe":
		locates, all := mgr.ResolveTickers(ctrl.Symbols)
		if all {
			c.SubscribeAll()
			log.Printf("client %d subscribed to all symbols", c.ID)
			// Send stock directory for all symbols
			sendStockDirectory(c, mgr, nil, true)
		} else if len(locates) > 0 {
			c.Subscribe(locates)
			log.Printf("client %d subscribed to %v", c.ID, ctrl.Symbols)
			sendStockDirectory(c, mgr, locates, false)
		}

	case "unsubscribe":
		locates, _ := mgr.ResolveTickers(ctrl.Symbols)
		if len(locates) > 0 {
			c.Unsubscribe(locates)
			log.Printf("client %d unsubscribed from %v", c.ID, ctrl.Symbols)
		}

	case "format":
		switch ctrl.Format {
		case "binary":
			c.SetFormat(FormatBinary)
			log.Printf("client %d switched to binary format", c.ID)
		case "json":
			c.SetFormat(FormatJSON)
			log.Printf("client %d switched to json format", c.ID)
		default:
			log.Printf("client %d unknown format: %s", c.ID, ctrl.Format)
		}

	default:
		log.Printf("client %d unknown action: %s", c.ID, ctrl.Action)
	}
}

// sendStockDirectory sends stock directory messages for subscribed symbols.
func sendStockDirectory(c *Client, mgr *Manager, locates []uint16, all bool) {
	syms := mgr.Symbols()
	var msgs []itch.Message

	for _, s := range syms {
		if !all {
			found := false
			for _, loc := range locates {
				if s.LocateCode == loc {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		msgs = append(msgs, itch.Message{
			Type:             itch.MsgStockDirectory,
			StockLocate:      s.LocateCode,
			Stock:            s.Ticker,
			MarketCategory:   'Q', // NASDAQ
			FinancialStatus:  'N', // Normal
			RoundLotSize:     100,
			RoundLotsOnly:    'N',
			IssueClassification: 'C', // Common stock
			IssueSubType:     [2]byte{'Z', ' '},
			Authenticity:     'P', // Live/production
			ShortSaleThreshold: 'N',
			IPOFlag:          ' ',
			LULDRefPriceTier: '1',
			ETPFlag:          'N',
			ETPLeverageFactor: 0,
			InverseIndicator: 'N',
		})
	}

	mgr.SendToClient(c, msgs)
}

// writePump sends messages from the send channel to the WebSocket.
func writePump(c *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case data, ok := <-c.SendCh():
			if !ok {
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))

			msgType := websocket.TextMessage
			if c.Format() == FormatBinary {
				msgType = websocket.BinaryMessage
			}

			if err := c.Conn.WriteMessage(msgType, data); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.Done():
			return
		}
	}
}
