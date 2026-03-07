# go-feed

Realistic market feed simulator. Streams ITCH 5.0 order book and trade data over WebSocket. 30 symbols across 8 sectors.

**The feed is live and publicly hosted — no setup required.** Just connect and subscribe:

- **WebSocket:** `wss://feed-sim.v3m.xyz/feed`
- **REST API:** `https://feed-sim.v3m.xyz/api/`
- **Health:** `https://feed-sim.v3m.xyz/health`

---

## Connect to the Feed

### WebSocket — live order book + trades

Subscribe to a symbol and start receiving order book messages immediately. No API key, no auth, no setup:

**JavaScript:**

```javascript
const ws = new WebSocket("wss://feed-sim.v3m.xyz/feed");
ws.onopen = () => ws.send(JSON.stringify({action: "subscribe", symbols: ["NEXO"]}));
ws.onmessage = (e) => console.log(JSON.parse(e.data));
```

**Python** (`pip install websockets`):

```python
import asyncio, json, websockets

async def main():
    async with websockets.connect("wss://feed-sim.v3m.xyz/feed") as ws:
        await ws.send(json.dumps({"action": "subscribe", "symbols": ["NEXO"]}))
        async for msg in ws:
            print(json.loads(msg))

asyncio.run(main())
```

**Node.js** (`npm install ws`):

```javascript
const { WebSocket } = require("ws");
const ws = new WebSocket("wss://feed-sim.v3m.xyz/feed");
ws.on("open", () => ws.send(JSON.stringify({action: "subscribe", symbols: ["NEXO"]})));
ws.on("message", (data) => console.log(JSON.parse(data)));
```

### Control Messages

```jsonc
{"action": "subscribe", "symbols": ["NEXO", "VALT"]}  // subscribe to specific symbols
{"action": "subscribe", "symbols": ["*"]}               // subscribe to all 30
{"action": "unsubscribe", "symbols": ["NEXO"]}          // unsubscribe
{"action": "format", "format": "binary"}                 // switch to binary ITCH 5.0
```

### Binary ITCH 5.0

The default format is JSON. Send `{"action": "format", "format": "binary"}` to switch to ITCH 5.0 binary wire format — the same encoding used by real exchange-level market data feeds.

Each WebSocket frame contains a 2-byte big-endian length prefix followed by the message body. Prices are 4-decimal fixed-point (`uint32`, multiply by `0.0001`). Timestamps are 6-byte big-endian nanoseconds since midnight UTC.

Use this if you're building or testing a feed handler that needs to parse real-world binary market data.

### REST API — historical data

```bash
curl https://feed-sim.v3m.xyz/api/symbols                              # all symbols + live prices
curl https://feed-sim.v3m.xyz/api/book/NEXO                            # order book depth
curl https://feed-sim.v3m.xyz/api/trades/NEXO?limit=20                 # recent trades
curl https://feed-sim.v3m.xyz/api/candles/NEXO?interval=5m&limit=50    # OHLCV candles
curl https://feed-sim.v3m.xyz/api/stats                                # aggregate stats
```

Candle intervals: `1m`, `5m`, `15m`, `1h`, `4h`, `1d`. Filter by time range with `from` and `to` (RFC3339).

### Message Types

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


### REST API Reference

All endpoints return JSON. Errors return `{"error": "message"}` with the appropriate HTTP status code.

| Endpoint | Description |
|----------|-------------|
| `GET /api/symbols` | All symbols with live prices and top-of-book |
| `GET /api/symbols/{ticker}` | Single symbol detail |
| `GET /api/book/{ticker}` | Order book depth (10 levels per side) |
| `GET /api/trades/{ticker}` | Paginated trades, newest first (max 1000) |
| `GET /api/candles/{ticker}` | OHLCV bars from trade history |
| `GET /api/stats` | Runtime and aggregate statistics |
| `GET /health` | Health check |

Query parameters for trades and candles:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 100 | Number of results (max 1000) |
| `offset` | int | 0 | Pagination offset (trades only) |
| `from` | RFC3339 | — | Start of time range |
| `to` | RFC3339 | — | End of time range |
| `interval` | string | `1m` | Candle bar size: `1m`, `5m`, `15m`, `1h`, `4h`, `1d` |

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

## Self-Hosting (optional)

Everything below is only needed if you want to run your own instance. The public feed at `wss://feed-sim.v3m.xyz/feed` requires no setup.

### Quick Start

```bash
cd go-feed
docker compose up -d
```

Starts PostgreSQL + feed simulator. Creates the database and tables on first run. Server listens on port 8100.

### Local Build (no Docker)

Requires Go 1.22+ and PostgreSQL 14+.

```bash
createdb feedsim
go build -o feedsim ./cmd/feedsim
./feedsim
```

### Configuration

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-port` | `FEED_PORT` | `8100` | HTTP/WebSocket listen port |
| `-host` | `FEED_HOST` | `0.0.0.0` | Listen address |
| `-database-url` | `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/feedsim?sslmode=disable` | PostgreSQL connection URL |
| `-seed` | `FEED_SEED` | `0` (random) | PRNG seed for reproducibility |
| `-send-buffer` | `SEND_BUFFER` | `4096` | Per-client WebSocket send buffer size |

Stress timing flags: `-stress-calm-min`, `-stress-calm-max`, `-stress-active-min`, `-stress-active-max`, `-stress-burst-min`, `-stress-burst-max` (all in milliseconds).

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
    binary.go              Binary encoder (ITCH 5.0 wire format)
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
| `github.com/jackc/pgx/v5` | PostgreSQL driver with connection pooling (pgxpool) |
