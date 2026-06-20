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
curl https://feed-sim.v3m.xyz/api/trades/NEXO,ACME?limit=50            # multi-symbol trades
curl https://feed-sim.v3m.xyz/api/trades/*                             # all symbols (market-wide)
curl https://feed-sim.v3m.xyz/api/candles/NEXO?interval=5m&limit=50    # OHLCV candles
curl https://feed-sim.v3m.xyz/api/stats                                # aggregate stats
```

Candle intervals: `1m`, `5m`, `15m`, `1h`, `4h`, `1d`. Filter by time range with `from` and `to` (RFC3339).

The trades endpoint accepts a single ticker (fast path), a comma-separated list (`NEXO,ACME`), or `*` for all symbols. Multi-symbol results are ordered newest-first with ticker as a stable tiebreak and bounded by the same `limit` clamp.

#### Historical lookback (live + archive)

Single-symbol `GET /api/trades/{ticker}` transparently spans the **live retention window** and the
**cold archive**: recent trades (within `TRADE_RETENTION_DAYS`) are served from PostgreSQL; older
trades are streamed from the gzipped archive on disk. Callers don't choose a source — results are a
single newest-first stream merged at the retention boundary. The archive is only read when the live
window doesn't already satisfy the page, so recent queries never touch disk.

- **Limits/paging:** `limit` is clamped to 1000 per request; merged `offset + limit` is likewise
  bounded at 1000. For deep history, page **by time**: pass `?to=<oldest executedAt seen>` to walk
  further back (the archive reader streams each day-file, so memory stays bounded regardless of range).
- **Available history bounds:** `GET /api/history/meta` →
  `{ retentionDays, archiveEnabled, archiveMinDay, archiveMaxDay }`. Archived lookback is
  disk-limited (oldest day = `archiveMinDay`; the archiver rotates out the oldest files past
  `ARCHIVE_MAX_GB`). Multi-symbol/`*` queries are live-only.

`GET /api/candles/{ticker}` likewise spans the boundary: bars for ranges predating the live window
are computed by streaming and bucketing the archived trades (the same OHLCV aggregation as the live
SQL path) and merged with live bars. The split is **day-aligned** at the newest archived day, so
every bar is sourced from exactly one store — no split or double-counted boundary bar. The interval
allow-list, the `before` cursor, `fill=zero`, and the 1000-row clamp all apply across the merge.

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
| `GET /api/trades/{ticker}` | Paginated trades, newest first (max 1000). `{ticker}` may be a single symbol, a comma-separated list, or `*` for all |
| `GET /api/candles/{ticker}` | OHLCV bars from trade history |
| `GET /api/stats` | Runtime and aggregate statistics |
| `GET /api/history/meta` | Available history: retention window + archived date bounds |
| `GET /health` | Health check |

Query parameters for trades and candles:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 100 | Number of results. Values above 1000 clamp to 1000; values ≤ 0 fall back to the default |
| `offset` | int | 0 | Pagination offset (trades only); negative values floor at 0 |
| `from` | RFC3339 | — | Start of time range |
| `to` | RFC3339 | — | End of time range |
| `interval` | string | `1m` | Candle bar size: `1m`, `5m`, `15m`, `1h`, `4h`, `1d` |
| `before` | RFC3339 | — | Candle pagination cursor: returns buckets starting strictly before this instant (newest-first) |
| `fill` | string | — | Candles only: `zero` emits zero-volume bars for empty buckets across the range; omit (or `none`) to skip gaps |

Malformed `limit`/`offset`/`from`/`to`/`interval`/`before`/`fill` values are rejected with `400 Bad Request` rather than being silently ignored.

**Candle pagination:** when a candle page is full (`limit` rows returned) the response carries an `X-Next-Cursor` header with the oldest bucket's timestamp. Pass it back as `?before=<cursor>` to fetch the next older page. Candles are computed on the fly (no rollup table) and capped at 1000 rows per page, including zero-filled bars.

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
| `-trade-retention` | `TRADE_RETENTION_DAYS` | `2` | Live trade-log retention in days, tuned to the 2 GiB budget (`0` = keep forever) |
| `-archive-dir` | `ARCHIVE_DIR` | `""` | Directory for cold trade archives (empty = archiving disabled) |
| `-archive-after` | `ARCHIVE_AFTER_HOURS` | `24` | Archive trades older than this many hours |

#### Storage budget

The live `trades` table is tuned against a hard **2 GiB** PostgreSQL budget. `TRADE_RETENTION_DAYS`
bounds how much trade history stays hot; older trades are pruned (and, when `ARCHIVE_DIR` is set,
rolled to cold gzipped NDJSON first). Watch usage against the budget via:

- `GET /health` — `dbSizeBytes`, `dbPctOf2GB`.
- `GET /api/stats` — `dbSizeBytes`, `dbTradesBytes`, `dbIndexBytes`, `dbPctOf2GB`, `dbBudgetBytes`.
- The retention loop logs DB size, percent of budget, and an estimated days-to-cap from recent growth each tick, and emits a one-shot **WARN** when usage crosses the 80% high-water mark (1.6 GiB).

**Retention math.** The default of **2 days** is derived from measurements of the default 30-symbol
simulation:

```
bytes/trade (heap + 2 indexes) ≈ 135 B    (measured ~133; const uses 150 with margin)
trade rate (steady state)      ≈ 63 /s     (measured; const uses 75 with margin)
=> ~0.68 GiB/day  ->  2 days ≈ 1.35 GiB ≈ 64% of the 2 GiB budget (under the 1.6 GiB headroom)
```

`SafeRetentionDays(bytesPerTrade, tradesPerSec, budgetBytes)` (in `internal/persist`) is the
formula; the measured figures fit a ~2.4-day window in the headroom, so 2 days leaves margin.
Tuned **time-based retention only** — there is no size-aware eviction.

**To retune:** read `dbTradesBytes`/`dbIndexBytes` and the trade count from `/api/stats` to get your
real bytes/trade and rate, plug them into the formula, and set `TRADE_RETENTION_DAYS` to the
result rounded **down**. Keep `TRADE_RETENTION_DAYS` greater than `ARCHIVE_AFTER_HOURS` (24h) so the
archiver rolls old trades to cold storage before retention deletes them. If `/health` trends toward
the 80% WARN, lower retention (and, if needed, `ARCHIVE_AFTER_HOURS`).

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
