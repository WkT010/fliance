import { useEffect, useRef } from 'react';
import { useMarketStore } from '@/store/marketStore';
import { get24hTickers, getOrderbook, getTrades } from '@/api/market';
import { socket } from '@/ws/socket';

export function useMarket(pair: string) {
  const store = useMarketStore();
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    store.setPair(pair);
    store.clearFills();
    getOrderbook(pair).then((ob) => { if (!ac.signal.aborted) store.setOrderbook(ob); });
    getTrades(pair).then((trades) => { if (!ac.signal.aborted) useMarketStore.setState({ trades }); });
    socket.connect(pair);
    intervalRef.current = setInterval(() => {
      get24hTickers().then((res) => {
        if (ac.signal.aborted) return;
        const map: Record<string, import('@/types').Ticker> = {};
        res.tickers.forEach((t) => (map[t.pair] = t));
        store.setTickers(map);
      });
    }, 5000);
    return () => { ac.abort(); if (intervalRef.current) clearInterval(intervalRef.current); socket.close(); };
  }, [pair, store]);
  return store;
}