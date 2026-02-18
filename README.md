# feed-simulator

Zero-dependency market feed simulator. Generates realistic tick and order book data over TCP using NDJSON (newline-delimited JSON).

30 symbols across 7 sectors. GBM price engine with sector-correlated returns. Full order book depth simulation with add/update/delete/trade lifecycle.

## Requirements

Node >= 18. No external dependencies.

## Quick Start

The simulator is a **TCP client** — it connects to a server you provide on the target port and streams messages.

```bash
# Start your TCP consumer on port 9001, then:
npm start                          # demo mode, 30 symbols, 10 ticks/sec

node index.js -t                   # deterministic test mode (seed=42)
node index.js --demo -n 5 -s 123   # 5 symbols, fixed seed
node index.js --demo --no-ob       # ticks only, no order book
node index.js -t --scenario burst  # run single test scenario
```

## CLI Options

```
Modes:
  --demo, -d           Continuous realistic feed (default)
  --test, -t           Deterministic test scenarios

Options:
  --host <host>        Target host (default: 127.0.0.1)
  --port, -p <port>    Target port (default: 9001)
  --symbols, -n <N>    Number of symbols, 1-30 (default: 30)
  --seed, -s <seed>    PRNG seed (default: Date.now for demo, 42 for test)
  --tick-ms <ms>       Tick interval in ms (default: 100)
  --duration <sec>     Run duration, 0 = indefinite (default: 0)
  --scenario <name>    Run single scenario in test mode
  --no-ob              Disable order book messages
  --verbose, -v        Echo messages to stdout
  --help, -h           Show help
```

## Protocol

NDJSON over TCP — each message is `JSON.stringify(obj) + '\n'`. The connection sets `TCP_NODELAY` for low latency.

### Tick Messages

Generated every tick interval for all active symbols. Prices follow geometric Brownian motion with sector correlation (0.6 factor).

```json
{"symbol":"AAPL","lastPrice":187.51,"volume":135,"bid":187.50,"ask":187.52}
```

| Field | Type | Description |
|-------|------|-------------|
| `symbol` | string | Ticker symbol |
| `lastPrice` | number | Last trade price |
| `volume` | number | Tick volume |
| `bid` | number | Best bid |
| `ask` | number | Best ask |

### Order Book Messages

Full depth-of-book simulation with add/update/delete/trade actions. Each symbol maintains ~60 orders across 20 price levels.

```json
{"type":"ob","action":"add","symbol":"AAPL","id":42,"side":"bid","price":187.49,"size":200,"priority":3}
{"type":"ob","action":"update","symbol":"AAPL","id":42,"size":150}
{"type":"ob","action":"delete","symbol":"AAPL","id":42}
{"type":"ob","action":"trade","symbol":"AAPL","price":187.50,"size":100,"aggressor":"buy"}
{"type":"ob","action":"ticksize","symbol":"AAPL","tickSize":0.01}
```

### Control Messages

Used in test scenarios for feed reset/rebuild testing.

```json
{"type":"control","action":"reset","symbol":"GOOGL","epoch":2}
```

## Symbols

30 symbols across 7 sectors with realistic base prices and per-symbol volatility multipliers (0.4x - 2.5x).

| Sector | Symbols |
|--------|---------|
| Tech | AAPL, MSFT, GOOGL, NVDA, TSLA, META |
| Finance | JPM, BAC, GS, MS, V |
| Healthcare | JNJ, PFE, UNH, ABBV |
| Energy | XOM, CVX, COP, SLB |
| Consumer | AMZN, KO, PG, WMT |
| Industrial | CAT, BA, GE, HON |
| ETFs | SPY, QQQ, IWM |

## Test Scenarios

Test mode (`-t`) sends deterministic, pre-built message sequences for validating downstream consumers.

| Scenario | Description | Messages |
|----------|-------------|----------|
| `singleSymbolLinear` | Basic tick ingestion — AAPL linear ramp | 10 |
| `obLifecycle` | Full OB CRUD: add, update, trade, delete | 14 |
| `multiSymbolInterleaved` | Per-symbol isolation (AAPL, MSFT, GOOGL) | 15 |
| `burst` | Stress test — 1000 rapid ticks (NVDA) | 1000 |
| `mixedFeed` | Interleaved tick + OB messages (MSFT) | 35 |
| `volumePattern` | Intraday U-shaped volume curve (SPY) | 78 |
| `obGapRecovery` | Feed reset + OB rebuild | 8 |

## Project Structure

```
index.js              CLI entry point — demo and test modes
lib/
  random.js           Seedable PRNG (mulberry32) + gaussian, weighted pick
  symbols.js          30-symbol trading universe
  connection.js       TCP connection wrapper, NDJSON sender, TCP_NODELAY
  market.js           GBM price engine with sector correlation
  orderbook.js        Order book depth simulator
  scenarios.js        7 deterministic test scenarios
test/
  smoke-test.js       E2E: feed -> subscribe AAPL.lastPrice -> verify updates
  big-tree-test.js    40+ subscription types, all worker functions
```

## Testing

Tests use raw TCP and manual WebSocket framing (RFC 6455) — no test framework or dependencies. They expect a downstream WebSocket server on port 8080 (the consumer, not part of this repo).

```bash
npm test              # smoke test (requires WS server on 8080)
```

## License

MIT
