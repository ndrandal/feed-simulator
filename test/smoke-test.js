#!/usr/bin/env node
'use strict';

// Minimal smoke test: 1 WS subscribe + 1 feed tick → expect 1 update
const net = require('net');
const crypto = require('crypto');

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

function wsSendRaw(socket, text) {
  const buf = Buffer.from(text, 'utf8');
  const mask = crypto.randomBytes(4);
  const len = buf.length;
  let header;
  if (len < 126) {
    header = Buffer.alloc(6);
    header[0] = 0x81; // FIN + text
    header[1] = 0x80 | len;
    mask.copy(header, 2);
  } else {
    header = Buffer.alloc(8);
    header[0] = 0x81;
    header[1] = 0x80 | 126;
    header.writeUInt16BE(len, 2);
    mask.copy(header, 4);
  }
  const masked = Buffer.alloc(len);
  for (let i = 0; i < len; i++) masked[i] = buf[i] ^ mask[i % 4];
  socket.write(Buffer.concat([header, masked]));
}

function parseWsFrames(buf) {
  const frames = [];
  let pos = 0;
  while (pos + 2 <= buf.length) {
    const opcode = buf[pos] & 0x0f;
    let plen = buf[pos + 1] & 0x7f;
    let hdrLen = 2;
    if (plen === 126) {
      if (pos + 4 > buf.length) break;
      plen = buf.readUInt16BE(pos + 2);
      hdrLen = 4;
    } else if (plen === 127) {
      if (pos + 10 > buf.length) break;
      plen = Number(buf.readBigUInt64BE(pos + 2));
      hdrLen = 10;
    }
    if (pos + hdrLen + plen > buf.length) break;
    const payload = buf.slice(pos + hdrLen, pos + hdrLen + plen);
    frames.push({ opcode, data: payload.toString('utf8') });
    pos += hdrLen + plen;
  }
  return { frames, remaining: buf.slice(pos) };
}

async function main() {
  const wsPort = parseInt(process.argv[2] || '8080');
  const feedPort = parseInt(process.argv[3] || '9001');

  console.log(`Smoke test: ws=:${wsPort} feed=:${feedPort}\n`);

  // 1. Connect feed (TCP)
  const feed = await new Promise((res, rej) => {
    const s = net.createConnection({ host: '127.0.0.1', port: feedPort }, () => res(s));
    s.on('error', rej);
    s.setNoDelay(true);
  });
  console.log('[feed] connected');

  // 2. Connect WS
  const wsKey = crypto.randomBytes(16).toString('base64');
  const ws = await new Promise((res, rej) => {
    const s = net.createConnection({ host: '127.0.0.1', port: wsPort }, () => {
      s.write(
        `GET / HTTP/1.1\r\nHost: 127.0.0.1:${wsPort}\r\n` +
        `Upgrade: websocket\r\nConnection: Upgrade\r\n` +
        `Sec-WebSocket-Key: ${wsKey}\r\nSec-WebSocket-Version: 13\r\n\r\n`
      );
      res(s);
    });
    s.on('error', rej);
  });

  // Wait for HTTP 101 upgrade
  let wsBuf = Buffer.alloc(0);
  let upgraded = false;
  const allJsonMsgs = [];

  await new Promise((resolve) => {
    ws.on('data', (chunk) => {
      wsBuf = Buffer.concat([wsBuf, chunk]);
      if (!upgraded) {
        const headerEnd = wsBuf.indexOf('\r\n\r\n');
        if (headerEnd < 0) return;
        const httpResp = wsBuf.slice(0, headerEnd).toString();
        if (!httpResp.includes('101')) {
          console.error('[ws] handshake FAILED:', httpResp);
          process.exit(1);
        }
        upgraded = true;
        wsBuf = wsBuf.slice(headerEnd + 4);
        console.log('[ws] connected (101 upgrade)');
        resolve();
        // Fall through to parse any trailing WS frames
      }
      if (upgraded) {
        const { frames, remaining } = parseWsFrames(wsBuf);
        wsBuf = remaining;
        for (const f of frames) {
          if (f.opcode === 1) {
            console.log(`[ws] rx: ${f.data}`);
            try { allJsonMsgs.push(JSON.parse(f.data)); } catch {}
          }
        }
      }
    });
  });

  // 3. Subscribe to AAPL.lastPrice
  console.log('\n[ws] subscribing key=1 AAPL.lastPrice ...');
  wsSendRaw(ws, JSON.stringify({
    type: 'subscribe',
    requests: [{ key: 1, symbol: 'AAPL', field: 'lastPrice' }]
  }));
  await sleep(300);

  // 4. Send ONE tick
  const tick = { symbol: 'AAPL', lastPrice: 187.42, volume: 350, bid: 187.40, ask: 187.44 };
  console.log(`\n[feed] sending tick: ${JSON.stringify(tick)}`);
  feed.write(JSON.stringify(tick) + '\n');
  await sleep(500);

  // 5. Send a few more ticks
  for (let i = 0; i < 5; i++) {
    const t = { symbol: 'AAPL', lastPrice: 187.50 + i * 0.1, volume: 100, bid: 187.49, ask: 187.51 };
    feed.write(JSON.stringify(t) + '\n');
  }
  await sleep(1000);

  // 6. Report
  console.log(`\n--- Results ---`);
  console.log(`Total WS messages: ${allJsonMsgs.length}`);
  const subscribed = allJsonMsgs.filter(m => m.type === 'subscribed');
  const updates = allJsonMsgs.filter(m => m.type === 'update');
  const errors = allJsonMsgs.filter(m => m.type === 'error');
  console.log(`  subscribed: ${subscribed.length}`);
  console.log(`  updates:    ${updates.length}`);
  console.log(`  errors:     ${errors.length}`);
  if (updates.length > 0) {
    console.log(`  first update: ${JSON.stringify(updates[0])}`);
    console.log(`  last update:  ${JSON.stringify(updates[updates.length - 1])}`);
  }
  if (errors.length > 0) {
    for (const e of errors) console.log(`  error: ${JSON.stringify(e)}`);
  }

  const pass = subscribed.length === 1 && updates.length > 0 && errors.length === 0;
  console.log(`\nResult: ${pass ? 'PASS' : 'FAIL'}`);

  feed.end();
  ws.end();
  await sleep(200);
  process.exit(pass ? 0 : 1);
}

main().catch(e => { console.error(e); process.exit(1); });
