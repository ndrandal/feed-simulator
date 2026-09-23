# CLAUDE.md — feed-simulator

> **Every claim below was measured in this tree on 2026-09-22** (ENC-1368), at `master`@`04a30ac`
> plus this branch, under `go version go1.27.0 linux/amd64`. Where a command's output is quoted,
> it was run. Do not add an instruction here you have not run — the point of this file is that the
> next session can trust it without re-deriving it.

## Project Overview

A **synthetic market-data feed simulator**: a Go service that generates a 30-symbol US-equity
order book from a GBM price engine, streams it as **ITCH 5.0** (JSON or binary) over a WebSocket,
persists every trade to **PostgreSQL**, and serves history back over a small REST API.

It has no upstream. There is **no outbound HTTP client anywhere in `go-feed/internal` or
`go-feed/cmd`**, and the module has exactly **two direct dependencies** — `gorilla/websocket` and
`jackc/pgx/v5`. Every number this service emits is computed here. That property is asserted by a
workspace gate (`specs/2026-09-14-local-data-plane/recheck.sh`, §4), so adding a fetch to
`internal/` or `cmd/` is a decision, not a refactor.

Within the platform it is the **upstream data source**: `tools/stack.sh up all` starts it and
points GMA_V3's `market.wsclient` ingress at `ws://localhost:8100/feed`. The public instance is
`wss://feed-sim.v3m.xyz/feed`. `README.md` (423 lines) is the user-facing document — protocol,
endpoints, symbol table, price model, retention math — and is a real design document; this file
does not restate it, it records what you need to *work in the repo*.

## Layout — the module is in `go-feed/`, not at the root

```
/                      README.md, ob-visualizer.html, .gitignore (one line: node_modules/)
go-feed/               THE GO MODULE — github.com/ndrandal/feed-simulator/go-feed
  go.mod               go 1.23, toolchain go1.24.2; 2 direct deps
  cmd/feedsim/         the server (one main.go, ~260 lines: wires everything, owns the runners)
  cmd/decoder/         CLI that connects to /feed and decodes it (binary by default)
  internal/            9 packages: api archive config engine itch orderbook persist session symbol
  Dockerfile           multi-stage, CGO_ENABLED=0, alpine runtime
  docker-compose.yml   caddy + feedsim + postgres — the HOSTED box's config, not a dev target
  Caddyfile            2 lines; the TLS terminator for feed-sim.v3m.xyz
  feed-viewer.js       102-line Node CLI (needs `ws`); defaults to the hosted feed
```

**Every `go` command runs from `go-feed/`.** There is no Mage, no `magefile.go`, no
`.golangci.yml`, and no `go.work`. `go-feed/feedsim` and `go-feed/decoder` are build output and are
gitignored (`go-feed/.gitignore:2,3`).

**There is no CI.** `git ls-tree -r HEAD --name-only | grep -c '^\.github'` → **0**. That is the
workspace-wide ruling (`specs/2026-09-14-deployability/SPEC.md` D8), and it is why the local gate
below is the only gate.

## Build, vet, test

```bash
cd go-feed
go build ./...     # clean
go vet ./...       # clean
go test ./...      # ONE pre-existing failure — see below
```

All three were run in this worktree. `build` and `vet` are silent and exit 0.

### `go test ./...` is RED at HEAD, and has been since before the archive work

21 `_test.go` files across the 9 `internal/` packages. `go test ./...` returns **exactly one**
failure and you should expect it:

```
--- FAIL: TestPhaseTransitions (0.01s)
    stress_test.go:100: expected all 3 phases, only saw 1
FAIL	github.com/ndrandal/feed-simulator/go-feed/internal/engine
```

Measured: **5 runs, 5 failures** — it is deterministic, not flaky (it seeds `NewRNG(42)` and forces
`phaseDuration = time.Nanosecond`). It also fails at **`4473188`**, the commit immediately
*preceding* the ten-ticket historical-access project — verified here by checking that commit out
into a throwaway worktree and running it. So the whole archive/retention project merged onto an
already-red suite, and with no CI nothing said so.

**What this means for you:** the green baseline for this repo is *"everything passes except
`TestPhaseTransitions`"*. A run that shows two failures is a regression; a run that shows this one
is not. Do not "fix" the suite by deleting it, and do not report a clean run you did not get.

### Database-backed tests skip silently

`internal/persist/integration_test.go` and `internal/archive/integration_test.go` call
`t.Skip("set TEST_DATABASE_URL …")` when that variable is unset — which is the default, so the
green rows above cover **unit tests only**. To actually run them:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/feedsim_test?sslmode=disable' \
  go test -p 1 ./...
```

`-p 1` is required, not advice: the archive and persist integration tests share the `trades` and
`sim_state` tables and will corrupt each other in parallel (`internal/archive/integration_test.go:13-15`).

## Running it

**Postgres is mandatory.** `cmd/feedsim/main.go:62` does `log.Fatalf` on a failed connection, so
the process exits 1 with no database — it does not degrade. Schema is applied in-process by
`store.Migrate` (DDL in `internal/persist/schema.go`); there is **no Atlas, no sqlc, no migration
directory**.

```bash
cd go-feed
go build -o feedsim ./cmd/feedsim && ./feedsim      # needs DATABASE_URL to resolve
docker compose up -d                                 # postgres + feedsim + caddy (the hosted shape)
```

One `http.ServeMux` on `FEED_PORT` (default **8100**), behind a permissive CORS middleware:
`/feed` (the WebSocket), `/health`, and `GET /api/{symbols,symbols/{ticker},book/{ticker},
trades/{ticker},candles/{ticker},stats,history/meta}` (`internal/api/api.go:57-64`,
`cmd/feedsim/main.go:125`).

Configuration is **flags + environment only** (`internal/config/config.go`) — no config file, no
`.env`. The full table is in `README.md`. The one default worth knowing here: **`ARCHIVE_DIR`
defaults to `""`, which disables the cold archive entirely**, so `internal/archive/`'s write path
is dead unless you opt in. It is set only in `docker-compose.yml:29`.

### From the workspace stack

`tools/stack.sh up feed` builds `./cmd/feedsim` **inside the shared checkout**
`<workspace>/feed-simulator/go-feed`, never in your worktree, and passes only `DATABASE_URL`,
`FEED_PORT` and `FEED_HOST`. Two consequences:

- The archive never runs under the local stack (`ARCHIVE_DIR` keeps its empty default), so
  `/api/history/meta` reports `archiveEnabled:false` and four of the ten historical-access tickets
  are unreachable in the only configuration the workspace ships.
- Its `[ -f go.sum ] || go mod tidy` guard is a no-op today: `go.sum` **is** committed. (The
  `Dockerfile` is the opposite case — it copies `go.mod` alone and runs `go mod tidy`, so the image
  build does not use the committed sums.)

## Working in this repo

- **The default branch is `master`, not `main`,** and `origin/main` does not exist. Anything that
  falls back `origin/main` → `main` reports every commit here as "not on the default branch"; that
  is a broken helper, not a verdict.
- **This repo merges with merge commits, not squashes**, so a ticket's commit is itself an ancestor
  of `master` and ordinary ancestry checks work.
- **Worktrees work here since ENC-1377:** `node tools/wt.mjs start ENC-nnn feed-simulator` branches
  off `master` and runs its setup hook inside `go-feed/`. Before that it exited `unknown repo`, and
  hand-rolling a worktree is still forbidden (workspace `CLAUDE.md`, *Parallel work with git worktrees*).
- **`ob-visualizer.html` lies when the server is down.** It is a standalone single-file page whose
  base URL defaults to the hosted host, and on **any** fetch error it silently switches to
  `useFake = true` and renders a synthetic book from `FAKE_SYMBOLS` (`ob-visualizer.html:430-437`),
  labelled only as `demo mode` in the status line. A plausible-looking book in that page is never
  evidence that a feed is running.
- **The symbol universe is synthetic US equities** (30 tickers, 8 sectors,
  `internal/symbol/symbol.go`). The platform's 2026-06 pivot is crypto-first, so this universe is
  not the target data — see the second pointer below.
- **A workspace gate greps this whole repo.** `specs/2026-09-16-base-only-seed-fixtures/recheck.sh`
  asserts that three ticker strings which are *not* in this simulator's universe appear in **zero**
  files anywhere under `feed-simulator/` — they are forum seed symbols that could never have ticked.
  Prose counts: naming them in a comment, a doc or a test fixture turns that row red without any
  behaviour changing. If you need to discuss them, do it in the Linear ticket, not in this tree.

## Design records — the umbrella D13 pointers (ENC-1368)

Two workspace-root design records decide things about this repo. They are **not** in this checkout:
each path below is relative to the workspace directory that holds `feed-simulator/` alongside
`forum/`, `embassy/` and `GMA_V3/` — e.g. `../specs/<dir>/SPEC.md` from here. No proposal directory
goes into a service repo, so these pointers are the only thing that makes those records reachable
from the code they govern (`specs/2026-09-14-gsd-proposal-backfill/SPEC.md` §D13, §D14). Each line
names the path **and what that record decides for feed-simulator specifically**.

Installed by **ENC-1368**, which supersedes the two one-line tickets **ENC-1237** and **ENC-1168** —
one per bullet, in the order they appear. Both of those SPECs say the pointer belongs in `README.md`
*"the repo has no `CLAUDE.md`"*; ENC-1377 created this file and re-scoped ENC-1368 to put them here
instead, which is where D13 wanted them.

- **`specs/2026-09-16-feed-sim-historical-access/SPEC.md` — §2 D1–D10, §3, §5 Q1–Q9** (ENC-1237).
  The only record wholly about this repo: all ten locked decisions and all ten tickets (32 points,
  one ~85-minute window on 2026-06-20, PRs #4 and #6–#14) are feed-simulator's, and the archive and
  retention code in this tree *is* that project. **D1** a hard **2 GiB** budget governs the live
  trade log and is held by **time-based retention alone** — size-aware eviction was considered and
  rejected (`internal/persist/size.go`: `SizeBudgetBytes`, `HeadroomBytes` 1.6 GiB, `HighWaterPct`
  80) · **D2** `TRADE_RETENTION_DAYS` defaults to **2**, *derived* from a measurement (~133 B/trade
  at ~63 trades/s ⇒ ~0.68 GiB/day, which made the old 7-day default 236% of the cap) with the
  in-code constants deliberately carrying margin over it (150 B, 75/s) · **D3** candles are computed
  on the fly and there is **no rollup table**, which is *why* the cursor is a time cursor
  (`?before=<RFC3339>`, returned as `X-Next-Cursor` only when a page came back full) · **D4**
  `fill=zero` is opt-in, the absent/`none` default is unchanged, and any other value is a 400 ·
  **D5** cold storage is gzipped NDJSON, one file per UTC day at
  `ARCHIVE_DIR/trades/YYYY/MM/DD.jsonl.gz`, written temp-file-plus-rename and streamed at both ends
  against the container's 450 MiB `GOMEMLIMIT` · **D6** the catalog only stats and walks the tree
  and **never decodes a file**, and degrades silently when archiving is off (the default) · **D7**
  archive access is transparent behind `persist.TradeReader`: the split is the retention cutoff, the
  archive is read only when the live page underfills, and the correctness argument is an
  *inequality* (retention > archive-after) rather than a merge rule · **D8** the candle merge is
  **day-aligned, not cutoff-aligned**, so no bar is ever assembled from both stores · **D9** an
  over-large `limit` **clamps to 1000** and is never rejected or silently reset, and a malformed
  parameter is a 400 · **D10** multi-symbol history is a selector on the existing `{ticker}` route
  (`A,B` or `*`), and is deliberately **live-only** — the archive layer overrides `QueryTrades` and
  `QueryCandles` but not `QueryTradesMulti`. Live for anyone editing this tree: §5 **Q2** (the
  workspace stack never sets `ARCHIVE_DIR`, so D5–D8 are dead code under `tools/stack.sh up feed`),
  **Q3** (nothing in the workspace calls a single historical endpoint — the capability has no caller
  and therefore no regression pressure), **Q5** (`/api/history/meta` advertises archive bounds that a
  `*` query cannot reach, and answers 200 with a silently short result), **Q7** (the red
  `TestPhaseTransitions` documented above) and **Q8** (zero-filled bars serialise as `…Z` while
  live bars carry the server's local offset, in the same array).

- **`specs/2026-09-14-local-data-plane/SPEC.md` — §2 D5, D7, §3, §5 Q7** (ENC-1168). This repo is a
  **fork source and a template, not a change** — its §3 row owes this tree no work, and that is the
  decision. **D5** rules the local `bitcoind`-facing wsserver *architecturally required* rather than
  a convenience (bitcoind speaks JSON-RPC and ZMQ, neither of which GMA's `WsFeedClient` can dial),
  and names **`go-feed/internal/session/`** — subscribe/cancel handling, the client registry, and
  per-client send buffers — as what ENC-1015 forks. ENC-1015 cites that path *without* the
  `go-feed/` module root; **the tree wins**, and a workspace gate asserts both that
  `go-feed/internal/session` exists and that `internal/session` does not. **D7** rules transport
  **WSS with Caddy terminating TLS in front**, and pins **`go-feed/Caddyfile:1-2`** as the
  production-proven template ENC-1017 copies (WebRTC DataChannel rejected on TURN cost and
  operational properties; WebTransport/HTTP-3 parked because unreliable datagrams cannot carry
  monotonic `offsetBytes`; SSE rejected). Its §3 also records the no-outbound-HTTP / two-direct-deps
  property quoted at the top of this file as the reason this repo is the ingestion-layer prior art
  (ENC-1011). **§5 Q7** is the *"feed-simulator is a seventh code repo that no workspace-level
  document lists"* finding — gitignored by the workspace repo and absent from `CLAUDE.md`'s
  *Repositories at a glance* table, so an agent handed ENC-1015 could not find the thing it was told
  to fork. **That finding was resolved on 2026-09-22 by ENC-1377**, which added the table row and
  taught `tools/wt.mjs` this repo; the gate rows asserting the old absence are stale by design and
  are owned by ENC-1392, not by this repo. Read it together with the other record's §5 Q4: the
  archive, the retention budget and the candle merge are tuned for a **synthetic US-equity** source
  that the crypto-first pivot has already ruled a fork source rather than the feed, and whether they
  survive that fork is written down nowhere.
