# go-feed

High-performance market feed simulator written in Go. Generates realistic ITCH 5.0 order book and trade data over WebSocket, with PostgreSQL persistence and a REST API for querying historical trades, OHLCV candles, and live book depth.

30 fictional symbols across 8 sectors. One stress symbol (BLITZ) cycles through calm / active / burst phases to generate variable-rate load for downstream testing.

---

## User Guide

### Requirements

- Go 1.22+
- PostgreSQL 14+ (any recent version works)

### Quick Start

```bash
# 1. Create the database
createdb feedsim

# 2. Build and run
cd go-feed
go build -o feedsim ./cmd/feedsim
./feedsim
```

The server starts on `0.0.0.0:8100` by default. On first run it creates all tables automatically.

### Configuration

All settings can be set via flags or environment variables.

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-port` | `FEED_PORT` | `8100` | HTTP/WebSocket listen port |
| `-host` | `FEED_HOST` | `0.0.0.0` | Listen address |
| `-db-host` | `DB_HOST` | `localhost` | PostgreSQL host |
| `-db-port` | `DB_PORT` | `5432` | PostgreSQL port |
| `-db-user` | `DB_USER` | `feedsim` | PostgreSQL user |
| `-db-pass` | `DB_PASSWORD` | `feedsim` | PostgreSQL password |
| `-db-name` | `DB_NAME` | `feedsim` | PostgreSQL database |
| `-db-ssl` | `DB_SSL` | `disable` | PostgreSQL SSL mode |
| `-seed` | `FEED_SEED` | `0` (random) | PRNG seed for reproducibility |
| `-send-buffer` | `SEND_BUFFER` | `4096` | Per-client WebSocket send buffer size |

Stress timing flags: `-stress-calm-min`, `-stress-calm-max`, `-stress-active-min`, `-stress-active-max`, `-stress-burst-min`, `-stress-burst-max` (all in milliseconds).

### WebSocket Feed

Connect to `ws://host:8100/feed` and send JSON control messages:

```jsonc
// Subscribe to specific symbols
{"action": "subscribe", "symbols": ["NEXO", "BLITZ"]}

// Subscribe to all 30 symbols
{"action": "subscribe", "symbols": ["*"]}

// Unsubscribe
{"action": "unsubscribe", "symbols": ["BLITZ"]}

// Switch to binary ITCH encoding (default is JSON)
{"action": "format", "format": "binary"}
```

On subscribe, the server sends a stock directory message for each symbol, then streams order book activity in real time.

#### JSON Message Types

| Type | Fields | Description |
|------|--------|-------------|
| `stock_directory` | `stockLocate`, `stock`, `marketCategory`, ... | Symbol metadata (sent once on subscribe) |
| `add_order` | `orderRef`, `side`, `shares`, `price`, `stock` | New limit order placed |
| `add_order_mpid` | Same + `mpid` | New order attributed to a market maker |
| `order_executed` | `orderRef`, `shares`, `matchNumber` | Passive order filled |
| `order_cancel` | `orderRef`, `shares` | Partial cancellation |
| `order_delete` | `orderRef` | Full order removal |
| `order_replace` | `origOrderRef`, `orderRef`, `shares`, `price` | Price/size modification |
| `trade` | `orderRef`, `side`, `shares`, `price`, `matchNumber` | Aggressive trade execution |
| `system_event` | `eventCode` | Market lifecycle events |
| `stock_trading_action` | `stock`, `tradingState` | Halt/resume notifications |

All messages include `timestamp` (nanoseconds since midnight UTC) and `stockLocate`.

#### Binary Format

Binary encoding follows the NASDAQ ITCH 5.0 specification. Each WebSocket frame contains a 2-byte big-endian length prefix followed by the message body. Prices are 4-decimal fixed-point (`uint32`, multiply by 0.0001`). Timestamps are 6-byte big-endian nanoseconds since midnight.

### REST API

All endpoints return JSON. Errors return `{"error": "message"}` with the appropriate HTTP status code.

#### Symbols

```
GET /api/symbols
```

Returns all 30 symbols with live prices and top-of-book:

```json
[
  {
    "locateCode": 1,
    "ticker": "NEXO",
    "name": "Nexo Dynamics Inc",
    "sector": "Tech",
    "price": 185.23,
    "bestBid": 185.22,
    "bestAsk": 185.24,
    "spread": 0.02
  }
]
```

```
GET /api/symbols/{ticker}
```

Returns a single symbol. Returns 404 if the ticker is not found.

#### Order Book Depth

```
GET /api/book/{ticker}
```

Returns up to 10 bid and 10 ask levels with aggregated order count and total shares:

```json
{
  "ticker": "BLITZ",
  "bids": [{"price": 125.03, "orders": 3, "totalShares": 1500}],
  "asks": [{"price": 125.04, "orders": 2, "totalShares": 800}],
  "bestBid": 125.03,
  "bestAsk": 125.04,
  "midPrice": 125.035,
  "spread": 0.01
}
```

#### Trades

```
GET /api/trades/{ticker}?limit=100&offset=0&from=2025-01-01T00:00:00Z&to=2025-12-31T23:59:59Z
```

Returns paginated trades from the database, newest first. Max limit is 1000.

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 100 | Number of trades to return (max 1000) |
| `offset` | int | 0 | Pagination offset |
| `from` | RFC3339 | — | Start of time range (inclusive) |
| `to` | RFC3339 | — | End of time range (inclusive) |

#### Candles (OHLCV)

```
GET /api/candles/{ticker}?interval=5m&limit=100&from=...&to=...
```

Returns OHLCV bars aggregated from the trades table.

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `interval` | string | `1m` | Bar size: `1m`, `5m`, `15m`, `1h`, `4h`, `1d` |
| `limit` | int | 100 | Number of bars to return (max 1000) |
| `from` | RFC3339 | — | Start of time range |
| `to` | RFC3339 | — | End of time range |

Response:

```json
[
  {"t": "2025-06-15T14:30:00Z", "o": 125.10, "h": 125.50, "l": 124.90, "c": 125.30, "v": 45200, "n": 87}
]
```

#### Stats

```
GET /api/stats
```

Returns runtime and aggregate statistics:

```json
{
  "uptime": "2h15m30s",
  "clients": 3,
  "symbols": 30,
  "totalOrders": 1842,
  "totalTrades": 52341,
  "totalVolume": 2847100
}
```

#### Health Check

```
GET /health
```

Returns `{"status":"ok","clients":N,"symbols":30}`.

### Decoder Tool

A companion CLI for inspecting the raw feed:

```bash
go build -o decoder ./cmd/decoder

# Subscribe to all symbols in binary mode (default)
./decoder

# Subscribe to specific symbols in JSON mode
./decoder -symbols BLITZ,NEXO -json

# Print message rate stats every 5 seconds
./decoder -stats 5

# Show hex dump alongside decoded output
./decoder -hex
```

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `ws://localhost:8100/feed` | WebSocket endpoint |
| `-symbols` | `*` | Comma-separated tickers or `*` for all |
| `-json` | `false` | Request JSON format instead of binary |
| `-stats` | `0` | Print msg/sec stats every N seconds (0 = off) |
| `-hex` | `false` | Print raw hex alongside decoded output |

### Symbols

30 symbols across 8 sectors with varying volatility:

| Sector | Symbols | Volatility |
|--------|---------|------------|
| Tech | NEXO, QBIT, FLUX, SYNK, PULS, CYRA | 1.2x - 1.7x |
| Finance | LEDG, VALT, CRDT, MNTX, FNDX | 0.6x - 0.9x |
| Healthcare | HELX, CURA, GENX, BIOS | 0.5x - 0.7x |
| Energy | VOLT, SOLR, FUSE, WATT | 1.0x - 1.2x |
| Consumer | BRND, LUXE, DLVR, RSTK | 0.7x - 0.9x |
| Industrial | FORG, BLDR, MACH, ALOY | 1.0x - 1.2x |
| Stress | BLITZ | 2.0x |
| ETF | MKTS, GRWT | 0.4x - 0.5x |

BLITZ is the stress symbol. It cycles through three phases with variable tick rates: calm (10-50ms), active (2-10ms), and burst (1-2ms). The transitions follow a sine wave with a random walk overlay.

---

## Developer Guide

### Project Structure

```
cmd/
  feedsim/main.go          Entry point — wires up all components, runs symbol loops
  decoder/main.go          CLI tool for inspecting the WebSocket feed
internal/
  api/
    api.go                 REST API server, routing, JSON helpers
    handlers.go            6 endpoint handlers (symbols, book, trades, candles, stats)
  config/config.go         Flag/env configuration loading
  engine/
    market.go              GBM price engine with sector-correlated returns
    random.go              PCG-XSH-RR PRNG, thread-safe, with Box-Muller gaussian
    stress.go              BLITZ phase controller (sine wave + random walk)
  itch/
    messages.go            ITCH 5.0 message types and constants
    binary.go              Binary encoder (NASDAQ wire format)
    json.go                JSON encoder (human-readable mirror)
  orderbook/
    order.go               Order struct, global atomic ID/match counters
    book.go                Price-time priority book with Depth() snapshot
    simulator.go           Action-weighted order book activity generator
  persist/
    store.go               PostgreSQL connection pool wrapper
    schema.go              DDL migration (symbols, orders, trades, sim_state)
    snapshot.go            Periodic state snapshotter + SaveTrade
    queries.go             Trade/candle/stats query functions
  session/
    client.go              WebSocket client with subscription tracking
    manager.go             Client registry, fan-out broadcaster
    handler.go             WebSocket upgrade, control message handling
```

### Architecture

```
                     ┌─────────────┐
                     │  MarketEngine│  GBM price ticks with
                     │  (engine/)   │  sector-correlated shocks
                     └──────┬──────┘
                            │ price
               ┌────────────┼────────────┐
               ▼            ▼            ▼
         symbolRunner  symbolRunner  stressRunner
         (100ms tick)  (100ms tick)  (1-50ms tick)
               │            │            │
               ▼            ▼            ▼
          ┌─────────┐ ┌─────────┐ ┌─────────┐
          │Simulator │ │Simulator │ │Simulator │  Weighted action
          │(orderbook)│ │(orderbook)│ │(orderbook)│  selection per tick
          └────┬─────┘ └────┬─────┘ └────┬─────┘
               │ msgs       │ msgs       │ msgs
               │            │            │
               ├──── tradeCh (buffered 4096) ────► 2 tradeWriter goroutines ──► PostgreSQL
               │            │            │
               ▼            ▼            ▼
          ┌──────────────────────────────────┐
          │          session.Manager          │  Fan-out to subscribed
          │  (encode once per format, then   │  WebSocket clients
          │   send to each matching client)  │
          └──────────────────────────────────┘
               │            │            │
          ws client    ws client    ws client

          ┌──────────────────────────────────┐
          │           REST API               │  /api/symbols, /api/book,
          │  (reads in-memory state + DB)    │  /api/trades, /api/candles,
          └──────────────────────────────────┘  /api/stats
```

### Price Model

Prices follow geometric Brownian motion (GBM):

```
S(t+1) = S(t) * exp(drift + vol * Z)
```

- `drift = 0` (no long-term trend)
- `vol = 0.02 / sqrt(86400) * symbol_multiplier` (2% annualized daily vol, scaled per tick)
- `Z = 0.6 * sector_shock + 0.4 * idiosyncratic_shock` (both standard normal)

Sector shocks are generated once per tick cycle and shared across all symbols in the same sector, producing realistic cross-symbol correlation.

### Order Book Simulation

Each tick, the simulator performs 1-10 weighted random actions on the book:

| Action | Weight | Description |
|--------|--------|-------------|
| Add | 30% | New limit order 1-10 ticks from mid |
| Cancel | 20% | Remove a random existing order |
| Replace | 15% | Modify price/size of a random order |
| Trade | 15% | Aggressive cross of the spread |
| Replenish | 20% | Add liquidity 1-5 ticks from mid |

The book maintains 10 price levels per side with price-time priority. Orders are optionally attributed to 8 market maker MPIDs (GSCO, MSCO, JPMS, etc.).

### Trade Persistence

Trade messages produced by `Simulator.Step()` are sent through a bounded channel (`chan tradeRecord`, capacity 4096) to 2 writer goroutines. This caps the number of concurrent database connections used for trade inserts at 2 (out of 10 in the pool), preventing goroutine pileup during BLITZ burst phases.

If the channel buffer fills (sustained burst exceeding write throughput), trades are silently dropped rather than blocking the ticker loop. This is a deliberate trade-off — the simulator prioritizes real-time feed latency over trade log completeness.

Separately, a `Snapshotter` runs on a 30-second interval and persists the full simulator state (symbol prices, all orders, PRNG state, counters) in a single transaction for crash recovery.

### Database Schema

Four tables, auto-created on startup:

- **`symbols`** — locate code, ticker, name, sector, base/current price, tick size, volatility
- **`orders`** — full order book snapshot (replaced entirely each snapshot cycle)
- **`trades`** — append-only trade log with `match_number` as primary key
- **`sim_state`** — key-value store for PRNG state and counters

Indexed: `trades(symbol_locate, executed_at)`, `orders(symbol_locate)`.

### PRNG

The simulator uses PCG-XSH-RR (not `math/rand`) for deterministic reproducibility. Pass `-seed N` to get identical price paths and order book activity across runs. State is persisted to PostgreSQL and restored on restart.

### Adding a Symbol

Add an entry to the `AllSymbols()` slice in `internal/symbol/symbol.go`:

```go
{31, "TICK", "My New Symbol Inc", SectorTech, 100.00, 0.01, 1.0, false},
```

The locate code must be unique. The symbol will automatically get its own book, runner goroutine, persistence, and API visibility on next restart.

### Adding an API Endpoint

1. Add a handler method to `internal/api/handlers.go`
2. Register the route in `Server.Register()` in `internal/api/api.go`
3. Use `writeJSON`/`writeError`/`resolveTicker` helpers for consistent responses

### Building

```bash
cd go-feed
go build -o feedsim ./cmd/feedsim
go build -o decoder ./cmd/decoder
```

### Dependencies

| Module | Purpose |
|--------|---------|
| `github.com/gorilla/websocket` | WebSocket server and client |
| `github.com/jackc/pgx/v5` | PostgreSQL driver with connection pooling |
