export const API_BASE = '/api/v2';
export const WS_URL = '/ws';

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
