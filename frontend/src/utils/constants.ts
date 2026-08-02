// API/WS base. Defaults to same-origin relative paths so the SPA works
// out-of-the-box when served by the api-gateway on one domain. For a
// split deployment (frontend on a CDN, API on another host), set
// VITE_API_BASE / VITE_WS_URL in frontend/.env.production.
export const API_BASE = import.meta.env.VITE_API_BASE || '/api/v2';
export const WS_URL = import.meta.env.VITE_WS_URL || '/ws';

export const DEFAULT_PAIR = 'BTC/USDT';
export const SUPPORTED_PAIRS = [
  'BTC/USDT',
  'ETH/USDT',
  'SOL/USDT',
  'BNB/USDT',
  'ADA/USDT',
];

export const INTERVALS = ['1m', '5m', '15m', '1h', '4h', '1d'];

export const WS_CHANNELS = {
  ORDERBOOK: 'orderbook',
  TRADES: 'trades',
  USER: 'user',
  TICKER: 'ticker',
} as const;
