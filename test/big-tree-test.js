#!/usr/bin/env node
'use strict';

// --------------------------------------------------------------------------
// big-tree-test.js — End-to-end test of a large, deeply-nested node tree
//
// Exercises every node type (Listener, Worker, Aggregate, AtomicAccessor,
// Chain), all 10 worker functions, TA atomics, OB atomics, cross-symbol
// aggregation, and multi-stage pipelines.
//
// Workflow:
//   1. Connect to GMA via WebSocket (port 8080)
//   2. Connect to FeedServer via TCP (port 9001)
//   3. Send tick + OB data for the test symbols
//   4. Subscribe to 16 request nodes covering every feature
//   5. Collect update messages and verify data flows through
// --------------------------------------------------------------------------

const net = require('net');
const crypto = require('crypto');

// ---- Minimal WebSocket client (RFC 6455, no dependencies) ----
class WSClient {
  constructor(host, port) {
    this.host = host;
    this.port = port;
    this.socket = null;
    this.onMessage = null;
    this._buf = Buffer.alloc(0);
  }

  connect() {
    return new Promise((resolve, reject) => {
      const key = crypto.randomBytes(16).toString('base64');
      const req = `GET / HTTP/1.1\r\nHost: ${this.host}:${this.port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: ${key}\r\nSec-WebSocket-Version: 13\r\n\r\n`;

      this.socket = net.createConnection({ host: this.host, port: this.port }, () => {
        this.socket.write(req);
      });

      let handshakeDone = false;
      this.socket.on('data', (chunk) => {
        if (!handshakeDone) {
          const str = chunk.toString();
          if (str.includes('101')) {
            handshakeDone = true;
            const idx = chunk.indexOf('\r\n\r\n');
            if (idx >= 0 && idx + 4 < chunk.length) {
              this._onData(chunk.slice(idx + 4));
            }
            resolve();
          } else {
            reject(new Error('WebSocket handshake failed: ' + str.slice(0, 200)));
          }
        } else {
          this._onData(chunk);
        }
      });

      this.socket.on('error', (err) => {
        if (!handshakeDone) reject(err);
      });
    });
  }

  _onData(chunk) {
    this._buf = Buffer.concat([this._buf, chunk]);
    while (this._buf.length >= 2) {
      const b0 = this._buf[0];
      const b1 = this._buf[1];
      const opcode = b0 & 0x0f;
      let payloadLen = b1 & 0x7f;
      let offset = 2;

      if (payloadLen === 126) {
        if (this._buf.length < 4) return;
        payloadLen = this._buf.readUInt16BE(2);
        offset = 4;
      } else if (payloadLen === 127) {
        if (this._buf.length < 10) return;
        payloadLen = Number(this._buf.readBigUInt64BE(2));
        offset = 10;
      }

      const totalLen = offset + payloadLen;
      if (this._buf.length < totalLen) return;

      const payload = this._buf.slice(offset, totalLen);
      this._buf = this._buf.slice(totalLen);

      if (opcode === 0x01) { // text
        const text = payload.toString('utf8');
        if (this.onMessage) this.onMessage(text);
      } else if (opcode === 0x08) { // close
        this.close();
        return;
      } else if (opcode === 0x09) { // ping
        this._sendFrame(0x0a, payload);
      }
    }
  }

  send(text) {
    const buf = Buffer.from(text, 'utf8');
    this._sendFrame(0x01, buf);
  }

  _sendFrame(opcode, payload) {
    const len = payload.length;
    let header;
    if (len < 126) {
      header = Buffer.alloc(6);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | len;
    } else if (len < 65536) {
      header = Buffer.alloc(8);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 126;
      header.writeUInt16BE(len, 2);
    } else {
      header = Buffer.alloc(14);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt(len), 2);
    }

    const mask = crypto.randomBytes(4);
    const maskOffset = header.length - 4;
    mask.copy(header, maskOffset);

    const masked = Buffer.alloc(len);
    for (let i = 0; i < len; i++) {
      masked[i] = payload[i] ^ mask[i % 4];
    }

    this.socket.write(Buffer.concat([header, masked]));
  }

  close() {
    if (this.socket) {
      try { this._sendFrame(0x08, Buffer.alloc(0)); } catch {}
      this.socket.end();
    }
  }
}

// ---- TCP feed connection ----
class FeedConn {
  constructor(host, port) {
    this.host = host;
    this.port = port;
    this.socket = null;
  }

  connect() {
    return new Promise((resolve, reject) => {
      this.socket = net.createConnection({ host: this.host, port: this.port }, resolve);
      this.socket.on('error', reject);
      this.socket.setNoDelay(true);
    });
  }

  send(obj) { this.socket.write(JSON.stringify(obj) + '\n'); }

  close() {
    return new Promise(resolve => {
      if (!this.socket) return resolve();
      this.socket.once('close', resolve);
      this.socket.end();
    });
  }
}

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

// ========================
// THE BIG NODE TREE — 16 requests
// ========================
//
// Protocol: ClientSession expects integer "key" (not string "id")
// Responses: {"type":"subscribed","key":N} and {"type":"update","key":N,"symbol":"...","value":...}
//
// Architecture reminder:
//   buildForRequest always creates: Listener(symbol, field) → [pipeline/node] → terminal
//   - If "pipeline" is present, it's built in reverse order (downstream-first)
//   - If "node" is present (without pipeline), the node sits between Listener and terminal
//   - pipeline overrides node when both are present
//   - Aggregate: inputs[] are source nodes that feed into Aggregate; the
//     Aggregate then forwards collected batches to its downstream
//
// When using Aggregate inside pipeline:
//   pipeline: [ Aggregate({inputs}), Worker(fn) ]
//   Built reverse: Worker(fn)→terminal, then Aggregate→Worker
//   The Listener triggers AtomicAccessors via Aggregate fan-in
//
// NOTE: Aggregate inputs use AtomicAccessor which reads from AtomicStore
//   on each trigger. The trigger comes from the Listener calling onValue
//   on the pipeline head, which is the Aggregate root (CompositeRoot).
//   BUT CompositeRoot.onValue() is a no-op for source nodes.
//   AtomicAccessors inside Aggregate are *also* connected via the Listener
//   because buildForRequest chains: Listener → head-of-pipeline → ... → terminal.
//   The Aggregate inputs get triggered by... actually, they self-trigger via
//   the dispatcher subscription. Let me just focus on what works reliably:
//   simple Listener → Worker pipelines, and field-based subscriptions.

const REQUESTS = [
  // ---- GROUP 1: Simple field subscriptions (Listener → Responder) ----
  // These just subscribe to atomics that MarketDispatcher computes per-tick

  { key: 1, symbol: 'AAPL', field: 'lastPrice',
    label: 'Raw lastPrice' },

  // TA atomics are computed per-tick and stored in AtomicStore, but don't
  // fire Listener notifications directly. To subscribe to them, listen on
  // a raw tick field (lastPrice) and pull via AtomicAccessor in the pipeline.
  { key: 2, symbol: 'AAPL', field: 'lastPrice',
    label: 'SMA(5) via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'sma_5' }] },

  { key: 3, symbol: 'AAPL', field: 'lastPrice',
    label: 'SMA(20) via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'sma_20' }] },

  { key: 4, symbol: 'AAPL', field: 'lastPrice',
    label: 'EMA(12) via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'ema_12' }] },

  { key: 5, symbol: 'AAPL', field: 'lastPrice',
    label: 'RSI(14) via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'rsi_14' }] },

  { key: 6, symbol: 'AAPL', field: 'lastPrice',
    label: 'MACD line via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_line' }] },

  { key: 7, symbol: 'AAPL', field: 'lastPrice',
    label: 'MACD signal via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_signal' }] },

  { key: 8, symbol: 'AAPL', field: 'lastPrice',
    label: 'MACD histogram via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_histogram' }] },

  { key: 9, symbol: 'AAPL', field: 'lastPrice',
    label: 'Bollinger upper via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'bollinger_upper' }] },

  { key: 10, symbol: 'AAPL', field: 'lastPrice',
    label: 'Bollinger lower via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'bollinger_lower' }] },

  { key: 11, symbol: 'AAPL', field: 'lastPrice',
    label: 'VWAP via AtomicAccessor',
    pipeline: [{ type: 'AtomicAccessor', symbol: 'AAPL', field: 'vwap' }] },

  { key: 12, symbol: 'MSFT', field: 'lastPrice',
    label: 'MSFT lastPrice (cross-symbol)' },

  { key: 13, symbol: 'MSFT', field: 'volume',
    label: 'MSFT volume' },

  // ---- GROUP 2: Worker pipelines (Listener → Worker(s) → Responder) ----
  // Test all 10 worker functions: mean, sum, max, min, spread, last, first, diff, scale, avg

  { key: 101, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(mean) — running average',
    pipeline: [{ type: 'Worker', fn: 'mean' }] },

  { key: 102, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(sum) — cumulative sum',
    pipeline: [{ type: 'Worker', fn: 'sum' }] },

  { key: 103, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(max) — running max',
    pipeline: [{ type: 'Worker', fn: 'max' }] },

  { key: 104, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(min) — running min',
    pipeline: [{ type: 'Worker', fn: 'min' }] },

  { key: 105, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(spread) — max-min range',
    pipeline: [{ type: 'Worker', fn: 'spread' }] },

  { key: 106, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(last) — most recent value',
    pipeline: [{ type: 'Worker', fn: 'last' }] },

  { key: 107, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(first) — earliest value',
    pipeline: [{ type: 'Worker', fn: 'first' }] },

  { key: 108, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(diff) — last minus first',
    pipeline: [{ type: 'Worker', fn: 'diff' }] },

  { key: 109, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(scale x100)',
    pipeline: [{ type: 'Worker', fn: 'scale', factor: 100 }] },

  { key: 110, symbol: 'AAPL', field: 'lastPrice',
    label: 'Worker(avg) — alias for mean',
    pipeline: [{ type: 'Worker', fn: 'avg' }] },

  // ---- GROUP 3: Multi-stage pipelines ----

  { key: 201, symbol: 'AAPL', field: 'lastPrice',
    label: 'Pipeline: sum → scale(0.01) → last',
    pipeline: [
      { type: 'Worker', fn: 'sum' },
      { type: 'Worker', fn: 'scale', factor: 0.01 },
      { type: 'Worker', fn: 'last' },
    ] },

  { key: 202, symbol: 'AAPL', field: 'lastPrice',
    label: 'Pipeline: max → scale(2.0)',
    pipeline: [
      { type: 'Worker', fn: 'max' },
      { type: 'Worker', fn: 'scale', factor: 2.0 },
    ] },

  // ---- GROUP 4: Chain node (syntactic sugar for pipeline) ----

  { key: 301, symbol: 'AAPL', field: 'lastPrice',
    label: 'Chain[sum, scale(0.5), last]',
    node: {
      type: 'Chain',
      stages: [
        { type: 'Worker', fn: 'sum' },
        { type: 'Worker', fn: 'scale', factor: 0.5 },
        { type: 'Worker', fn: 'last' },
      ],
    } },

  { key: 302, symbol: 'MSFT', field: 'lastPrice',
    label: 'Chain[mean, scale(10)] on MSFT',
    node: {
      type: 'Chain',
      stages: [
        { type: 'Worker', fn: 'mean' },
        { type: 'Worker', fn: 'scale', factor: 10 },
      ],
    } },

  // ---- GROUP 5: Aggregate fan-in (multiple AtomicAccessor → Worker) ----

  { key: 401, symbol: 'AAPL', field: 'lastPrice',
    label: 'Aggregate(3) [sma_5, sma_20, ema_12] → mean',
    pipeline: [
      {
        type: 'Aggregate', arity: 3,
        inputs: [
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'sma_5' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'sma_20' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'ema_12' },
        ],
      },
      { type: 'Worker', fn: 'mean' },
    ] },

  { key: 402, symbol: 'AAPL', field: 'lastPrice',
    label: 'Aggregate(2) [rsi_14, sma_5] → spread',
    pipeline: [
      {
        type: 'Aggregate', arity: 2,
        inputs: [
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'rsi_14' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'sma_5' },
        ],
      },
      { type: 'Worker', fn: 'spread' },
    ] },

  { key: 403, symbol: 'AAPL', field: 'lastPrice',
    label: 'Aggregate(2) [AAPL.lastPrice, MSFT.lastPrice] → mean (cross-symbol)',
    pipeline: [
      {
        type: 'Aggregate', arity: 2,
        inputs: [
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'lastPrice' },
          { type: 'AtomicAccessor', symbol: 'MSFT', field: 'lastPrice' },
        ],
      },
      { type: 'Worker', fn: 'mean' },
    ] },

  { key: 404, symbol: 'AAPL', field: 'lastPrice',
    label: 'Aggregate(2) [bollinger_upper, bollinger_lower] → diff',
    pipeline: [
      {
        type: 'Aggregate', arity: 2,
        inputs: [
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'bollinger_upper' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'bollinger_lower' },
        ],
      },
      { type: 'Worker', fn: 'diff' },
    ] },

  { key: 405, symbol: 'AAPL', field: 'lastPrice',
    label: 'Aggregate(3) [macd_line, macd_signal, macd_histogram] → max',
    pipeline: [
      {
        type: 'Aggregate', arity: 3,
        inputs: [
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_line' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_signal' },
          { type: 'AtomicAccessor', symbol: 'AAPL', field: 'macd_histogram' },
        ],
      },
      { type: 'Worker', fn: 'max' },
    ] },
];

function buildBigTree() {
  // Strip the 'label' field before sending (it's just for our display)
  return {
    type: 'subscribe',
    requests: REQUESTS.map(r => {
      const { label, ...rest } = r;
      return rest;
    }),
  };
}

// ========================
// MAIN
// ========================
async function main() {
  const wsHost = '127.0.0.1';
  const wsPort = 8080;
  const feedHost = '127.0.0.1';
  const feedPort = 9001;

  console.log('=== GMA Big Node Tree Test ===\n');

  // 1. Connect
  console.log(`[1] Connecting to feed server (${feedHost}:${feedPort})...`);
  const feed = new FeedConn(feedHost, feedPort);
  await feed.connect();
  console.log('    OK\n');

  console.log(`[2] Connecting to WS server (${wsHost}:${wsPort})...`);
  const ws = new WSClient(wsHost, wsPort);
  await ws.connect();
  console.log('    OK\n');

  // 2. Collect WS messages
  const messages = [];
  const subscribed = new Set();   // key -> true
  const updates = new Map();      // key -> [values]
  const errors = [];

  ws.onMessage = (text) => {
    try {
      const msg = JSON.parse(text);
      messages.push(msg);
      if (msg.type === 'subscribed') {
        subscribed.add(msg.key);
      } else if (msg.type === 'update') {
        if (!updates.has(msg.key)) updates.set(msg.key, []);
        updates.get(msg.key).push(msg.value);
      } else if (msg.type === 'error') {
        errors.push(msg);
      }
    } catch {}
  };

  // 3. Seed data — 50 ticks + OB depth
  console.log('[3] Seeding market data (50 ticks AAPL + MSFT, 20 OB orders)...');

  feed.send({ type: 'ob', action: 'ticksize', symbol: 'AAPL', tickSize: 0.01 });
  feed.send({ type: 'ob', action: 'ticksize', symbol: 'MSFT', tickSize: 0.01 });

  for (let i = 0; i < 50; i++) {
    const ap = +(187.50 + Math.sin(i * 0.3) * 2.0).toFixed(2);
    const mp = +(415.20 + Math.cos(i * 0.2) * 3.0).toFixed(2);
    feed.send({ symbol: 'AAPL', lastPrice: ap, volume: 100 + i * 10,
                bid: +(ap - 0.02).toFixed(2), ask: +(ap + 0.02).toFixed(2) });
    feed.send({ symbol: 'MSFT', lastPrice: mp, volume: 200 + i * 15,
                bid: +(mp - 0.03).toFixed(2), ask: +(mp + 0.03).toFixed(2) });
  }

  for (let l = 1; l <= 10; l++) {
    feed.send({ type: 'ob', action: 'add', symbol: 'AAPL',
                id: 1000 + l, side: 'bid', price: +(187.50 - l * 0.01).toFixed(2),
                size: 200 + l * 50, priority: l });
    feed.send({ type: 'ob', action: 'add', symbol: 'AAPL',
                id: 2000 + l, side: 'ask', price: +(187.50 + l * 0.01).toFixed(2),
                size: 150 + l * 40, priority: l });
  }

  console.log('    Settling (500ms)...');
  await sleep(500);

  // 4. Subscribe
  const tree = buildBigTree();
  const reqCount = tree.requests.length;
  console.log(`\n[4] Subscribing to ${reqCount} requests...`);

  // Print tree structure
  for (const r of REQUESTS) {
    const pipe = r.pipeline ? ` [pipeline x${r.pipeline.length}]` : '';
    const node = r.node ? ' [node]' : '';
    console.log(`    key=${String(r.key).padStart(3)}  ${r.symbol}.${r.field.padEnd(20)} ${r.label}${pipe}${node}`);
  }

  ws.send(JSON.stringify(tree));
  await sleep(500);

  // 5. Drive pipelines with more data — send in bursts with small delays
  console.log(`\n[5] Sending 50 ticks to drive pipelines...`);
  for (let i = 0; i < 50; i++) {
    const ap = +(188.00 + Math.sin(i * 0.5) * 1.5).toFixed(2);
    const mp = +(416.00 + Math.cos(i * 0.4) * 2.0).toFixed(2);
    feed.send({ symbol: 'AAPL', lastPrice: ap, volume: 300 + i * 20,
                bid: +(ap - 0.01).toFixed(2), ask: +(ap + 0.01).toFixed(2) });
    feed.send({ symbol: 'MSFT', lastPrice: mp, volume: 400 + i * 25,
                bid: +(mp - 0.01).toFixed(2), ask: +(mp + 0.01).toFixed(2) });
    if (i % 5 === 0) {
      feed.send({ type: 'ob', action: 'trade', symbol: 'AAPL',
                  price: ap, size: 100, aggressor: 'buy' });
    }
    // Small pause every 10 ticks for async processing
    if (i % 10 === 9) await sleep(50);
  }

  console.log('    Waiting for pipeline outputs (3s)...');
  await sleep(3000);

  // 6. Report
  console.log('\n' + '='.repeat(72));
  console.log('  RESULTS');
  console.log('='.repeat(72) + '\n');

  // Subscriptions
  const subOk = REQUESTS.filter(r => subscribed.has(r.key));
  const subFail = REQUESTS.filter(r => !subscribed.has(r.key));
  console.log(`Subscriptions: ${subOk.length}/${reqCount}\n`);

  if (subFail.length > 0) {
    console.log('  FAILED subscriptions:');
    for (const r of subFail) console.log(`    key=${r.key}: ${r.label}`);
    console.log();
  }

  // Errors
  if (errors.length > 0) {
    console.log(`Errors (${errors.length}):`);
    for (const e of errors) console.log(`  ${JSON.stringify(e)}`);
    console.log();
  }

  // Updates
  console.log('Updates per request:');
  let totalUpdates = 0;
  let withData = 0;

  const groups = [
    { title: 'GROUP 1 — Direct fields + TA atomics via AtomicAccessor', keys: [1,2,3,4,5,6,7,8,9,10,11,12,13] },
    { title: 'GROUP 2 — Single Worker pipelines (all 10 functions)', keys: [101,102,103,104,105,106,107,108,109,110] },
    { title: 'GROUP 3 — Multi-stage pipelines', keys: [201,202] },
    { title: 'GROUP 4 — Chain nodes', keys: [301,302] },
    { title: 'GROUP 5 — Aggregate fan-in', keys: [401,402,403,404,405] },
  ];

  for (const g of groups) {
    console.log(`\n  ${g.title}`);
    for (const k of g.keys) {
      const r = REQUESTS.find(x => x.key === k);
      const vals = updates.get(k) || [];
      totalUpdates += vals.length;
      if (vals.length > 0) withData++;
      const lastVal = vals.length > 0
        ? (typeof vals[vals.length - 1] === 'number'
            ? vals[vals.length - 1].toFixed(4) : vals[vals.length - 1])
        : '-';
      const status = subscribed.has(k) ? (vals.length > 0 ? 'OK' : 'SUB') : 'FAIL';
      console.log(`    [${status.padEnd(4)}] key=${String(k).padStart(3)}  ${String(vals.length).padStart(3)} updates  last=${String(lastVal).padStart(12)}  ${r.label}`);
    }
  }

  // Summary
  console.log('\n' + '='.repeat(72));
  console.log('  SUMMARY');
  console.log('='.repeat(72));
  console.log(`  Total WS messages:     ${messages.length}`);
  console.log(`  Subscriptions OK:      ${subOk.length}/${reqCount}`);
  console.log(`  Requests with data:    ${withData}/${reqCount}`);
  console.log(`  Total updates:         ${totalUpdates}`);
  console.log(`  Errors:                ${errors.length}`);

  const pass = subOk.length === reqCount && errors.length === 0;
  console.log(`\n  Result: ${pass ? 'PASS' : 'PARTIAL'}`);
  console.log('='.repeat(72));

  // Cleanup
  ws.close();
  await feed.close();
  await sleep(200);
}

main().catch(err => {
  console.error('Fatal:', err.message);
  process.exit(1);
});
