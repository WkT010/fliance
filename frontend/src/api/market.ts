import { api } from './client';
import type { Ticker, Orderbook, Trade, Candle, PriceComparison } from '@/types';

export async function getTickers(): Promise<{ tickers: Ticker[]; count: number }> {
  const res = await api.get('/tickers');
  return res.data;
}

export async function get24hTickers(): Promise<{ tickers: Ticker[] }> {
  const res = await api.get('/tickers/24h');
  return res.data;
}

export async function getOrderbook(pair: string): Promise<Orderbook> {
  const res = await api.get(`/orderbook/${encodeURIComponent(pair)}`);
  return res.data;
}

export async function getTrades(pair: string, limit = 50): Promise<Trade[]> {
  const res = await api.get(`/trades/${encodeURIComponent(pair)}?limit=${limit}`);
  return res.data.trades || [];
}

export async function getCandles(pair: string, interval: string, limit = 200): Promise<Candle[]> {
  // The server may still be backfilling this (pair, interval) series from the
  // external mirror on first use; retry twice with a short backoff so the
  // chart recovers without a manual page reload.
  let lastErr: unknown;
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const res = await api.get(`/klines/${encodeURIComponent(pair)}?interval=${interval}&limit=${limit}`);
      return res.data.candles || [];
    } catch (err) {
      lastErr = err;
      if (attempt < 2) await new Promise((r) => setTimeout(r, 1000 * (attempt + 1)));
    }
  }
  throw lastErr;
}

export async function getPriceComparison(pair: string): Promise<PriceComparison> {
  const res = await api.get(`/price/compare/${encodeURIComponent(pair)}`);
  return res.data;
}

export async function getUniswapTicker(pair: string): Promise<Ticker> {
  const res = await api.get(`/price/uniswap/${encodeURIComponent(pair)}`);
  return res.data;
}
