import { useEffect, useRef } from 'react';
import { useMarketStore } from '@/store/marketStore';
import { get24hTickers, getOrderbook, getTrades } from '@/api/market';
import { socket } from '@/ws/socket';

export function useMarket(pair: string) {
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    // Cancel any in-flight requests from previous pair
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;

    const store = useMarketStore.getState();
    store.setPair(pair);
    store.clearFills();

    // initial snapshot — use AbortController to prevent race conditions
    getOrderbook(pair).then((ob) => {
      if (!ac.signal.aborted) store.setOrderbook(ob);
    });
    getTrades(pair).then((trades) => {
      if (!ac.signal.aborted) useMarketStore.setState({ trades });
    });

    socket.connect(pair);

    intervalRef.current = setInterval(() => {
      get24hTickers().then((res) => {
        if (ac.signal.aborted) return;
        const map: Record<string, import('@/types').Ticker> = {};
        res.tickers.forEach((t) => (map[t.pair] = t));
        useMarketStore.setState({ tickers: map });
      });
    }, 5000);

    return () => {
      ac.abort();
      if (intervalRef.current) clearInterval(intervalRef.current);
      socket.close();
    };
  }, [pair]);

  return useMarketStore();
}

