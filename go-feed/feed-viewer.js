#!/usr/bin/env node
'use strict';

const { WebSocket } = require('ws');

const url = process.argv[2] || 'wss://feed-sim.v3m.xyz/feed';
const symbol = process.argv[3] || 'NEXO';

const RESET = '\x1b[0m';
const DIM = '\x1b[2m';
const GREEN = '\x1b[32m';
const RED = '\x1b[31m';
const YELLOW = '\x1b[33m';
const CYAN = '\x1b[36m';
const MAGENTA = '\x1b[35m';
const WHITE = '\x1b[37;1m';

const TYPE_COLOR = {
  stock_directory: CYAN,
  add_order: GREEN,
  add_order_mpid: GREEN,
  order_executed: YELLOW,
  order_cancel: RED,
  order_delete: RED,
  order_replace: MAGENTA,
  trade: WHITE,
  system_event: CYAN,
  stock_trading_action: MAGENTA,
};

function fmtPrice(p) {
  return p != null ? Number(p).toFixed(2).padStart(10) : '       N/A';
}

function fmt(msg) {
  const type = msg.type || 'unknown';
  const color = TYPE_COLOR[type] || DIM;
  const ts = new Date().toLocaleTimeString('en-US', { hour12: false });
  const parts = [`${DIM}${ts}${RESET}`, `${color}${type.padEnd(18)}${RESET}`];

  switch (type) {
    case 'stock_directory':
      parts.push(`${msg.stock}`);
      break;
    case 'add_order':
    case 'add_order_mpid':
      parts.push(side(msg.side), `${CYAN}${String(msg.shares).padStart(6)}${RESET}`, '@', `${YELLOW}${fmtPrice(msg.price)}${RESET}`);
      if (msg.mpid) parts.push(`${DIM}${msg.mpid}${RESET}`);
      break;
    case 'order_executed':
      parts.push(`${CYAN}${String(msg.shares).padStart(6)}${RESET}`, 'filled', `${DIM}match=${msg.matchNumber}${RESET}`);
      break;
    case 'order_cancel':
      parts.push(`${CYAN}${String(msg.shares).padStart(6)}${RESET}`, 'cancelled');
      break;
    case 'order_delete':
      parts.push(`ref=${msg.orderRef}`);
      break;
    case 'order_replace':
      parts.push(`${msg.origOrderRef}->${msg.orderRef}`, `${CYAN}${String(msg.shares).padStart(6)}${RESET}`, '@', `${YELLOW}${fmtPrice(msg.price)}${RESET}`);
      break;
    case 'trade':
      parts.push(side(msg.side), `${CYAN}${String(msg.shares).padStart(6)}${RESET}`, '@', `${YELLOW}${fmtPrice(msg.price)}${RESET}`);
      break;
    default:
      parts.push(JSON.stringify(msg));
  }

  return parts.join(' ');
}

function side(s) {
  return s === 'B' ? `${GREEN} BUY${RESET}` : `${RED}SELL${RESET}`;
}

console.log(`connecting to ${url} | symbol: ${symbol}`);

const ws = new WebSocket(url);

ws.on('open', () => {
  console.log(`${GREEN}connected${RESET} - streaming ${symbol}\n`);
  ws.send(JSON.stringify({ action: 'subscribe', symbols: [symbol] }));
});

ws.on('message', (data) => {
  const msg = JSON.parse(data);
  console.log(fmt(msg));
});

ws.on('close', () => {
  console.log(`\n${RED}disconnected${RESET}`);
  process.exit(0);
});

ws.on('error', (err) => {
  console.error(`${RED}error: ${err.message}${RESET}`);
  process.exit(1);
});

process.on('SIGINT', () => {
  ws.close();
});
