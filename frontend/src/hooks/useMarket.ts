import { useEffect, useRef } from 'react';
import { useMarketStore } from '@/store/marketStore';
import { get24hTickers, getOrderbook, getTrades } from '@/api/market';
import { socket } from '@/ws/socket';

export function useMarket(pair: string) {
  const store = useMarketStore();
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    store.setPair(pair);
    store.clearFills();

    // initial snapshot
    getOrderbook(pair).then((ob) => store.setOrderbook(ob));
    getTrades(pair).then((trades) => {
      store.trades = trades;
      useMarketStore.setState({ trades });
    });

    socket.connect(pair);

    intervalRef.current = setInterval(() => {
      get24hTickers().then((res) => {
        const map: Record<string, import('@/types').Ticker> = {};
        res.tickers.forEach((t) => (map[t.pair] = t));
        store.setTickers(map);
      });
    }, 5000);

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
      socket.close();
    };
  }, [pair, store]);

  return store;
}
