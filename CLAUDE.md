# Feed Simulator

Zero-dependency market feed simulator — generates realistic tick + order book data over TCP (NDJSON protocol).

Extracted from GMA_V3.

## Quick Reference

```bash
npm start          # run in demo mode (node index.js --demo)
npm test           # smoke test (node test/smoke-test.js)
node index.js -t   # deterministic test mode (seed=42)
```

## Project Structure

```
index.js              CLI entry point — demo mode (continuous) and test mode (scenarios)
lib/
  random.js           Seedable PRNG (mulberry32) with gaussian, weighted pick, etc.
  symbols.js          30-symbol trading universe across 7 sectors
  connection.js       TCP connection wrapper, sends NDJSON (JSON + newline), TCP_NODELAY
  market.js           GBM price engine with sector-correlated returns
  orderbook.js        Order book depth simulator (add/update/delete/trade actions)
  scenarios.js        7 deterministic test scenarios for validation
test/
  smoke-test.js       Minimal e2e: feed → subscribe AAPL.lastPrice → verify updates (WS on 8080)
  big-tree-test.js    40+ subscription types, all worker functions, cross-symbol aggregations
```

## Key Conventions

- **Node >= 18**, zero external dependencies (only `net`, `crypto`, `buffer`)
- **CommonJS** (`require`/`module.exports`), strict mode
- **NDJSON over TCP** — each message is `JSON.stringify(obj) + '\n'`
- Tests expect a separate WS server on port 8080 (the consumer, not part of this repo)
- Feed server default: `127.0.0.1:9001`

## Message Formats

**Tick:** `{symbol, lastPrice, volume, bid, ask}`

**Order book:** `{type: "ob", action: "ticksize"|"add"|"update"|"delete"|"trade", symbol, ...}`

**Control:** `{type: "control", action: "reset", symbol, epoch}`

## Testing

Tests use raw TCP + manual WebSocket (RFC 6455) — no test framework. They connect to both the feed port (9001) and a downstream WS server (8080). The WS server is external and must be running for tests to pass.
