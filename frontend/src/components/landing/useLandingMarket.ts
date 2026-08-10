import { useEffect, useState } from 'react';
import { get24hTickers } from '@/api/market';
import { getAmmPools, type AmmPool } from '@/api/amm';
import type { Ticker } from '@/types';

export interface LandingMarketData {
  tickers: Ticker[];
  pools: AmmPool[];
  /** Sum of quote_volume_24h across all pairs (USDT). */
  totalQuoteVolume: number;
  /** Estimated AMM TVL: USDT reserve + base reserve valued at last price. */
  ammTvl: number;
  loading: boolean;
}

function toNum(v: string | undefined): number {
  if (!v) return 0;
  const n = parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

/**
 * Polls /api/v2/tickers/24h (5s) and /api/v2/amm/pools (30s) for the landing
 * page hero stats and market lists. Both endpoints are public.
 */
export function useLandingMarket(): LandingMarketData {
  const [tickers, setTickers] = useState<Ticker[]>([]);
  const [pools, setPools] = useState<AmmPool[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    const loadTickers = async () => {
      try {
        const res = await get24hTickers();
        if (!cancelled) {
          setTickers(res.tickers || []);
          setLoading(false);
        }
      } catch {
        /* keep last good data */
      }
    };

    const loadPools = async () => {
      try {
        const list = await getAmmPools();
        if (!cancelled) setPools(list || []);
      } catch {
        /* pools are optional decoration */
      }
    };

    void loadTickers();
    void loadPools();
    const tickerId = setInterval(loadTickers, 5000);
    const poolId = setInterval(loadPools, 30000);
    return () => {
      cancelled = true;
      clearInterval(tickerId);
      clearInterval(poolId);
    };
  }, []);

  const lastPrice = new Map<string, number>();
  for (const tk of tickers) lastPrice.set(tk.pair, toNum(tk.last));

  const totalQuoteVolume = tickers.reduce((acc, tk) => acc + toNum(tk.quote_volume_24h), 0);

  const ammTvl = pools.reduce((acc, p) => {
    const reserve1 = toNum(p.reserve1); // quote side (USDT)
    const reserve0 = toNum(p.reserve0);
    const price = lastPrice.get(p.pair) ?? 0;
    return acc + reserve1 + (price > 0 ? reserve0 * price : reserve1);
  }, 0);

  return { tickers, pools, totalQuoteVolume, ammTvl, loading };
}
