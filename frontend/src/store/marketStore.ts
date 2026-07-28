import { create } from 'zustand';
import type { Ticker, Orderbook, Trade, FillMessage } from '@/types';

interface MarketState {
  currentPair: string;
  tickers: Record<string, Ticker>;
  orderbook: Orderbook | null;
  trades: Trade[];
  fills: FillMessage[];
  setPair: (pair: string) => void;
  setTickers: (tickers: Record<string, Ticker>) => void;
  updateTicker: (pair: string, patch: Partial<Ticker>) => void;
  setOrderbook: (ob: Orderbook) => void;
  addTrade: (trade: Trade) => void;
  addFill: (fill: FillMessage) => void;
  clearFills: () => void;
}

export const useMarketStore = create<MarketState>((set) => ({
  currentPair: 'BTC/USDT',
  tickers: {},
  orderbook: null,
  trades: [],
  fills: [],
  setPair: (pair) => set({ currentPair: pair }),
  setTickers: (tickers) => set({ tickers }),
  updateTicker: (pair, patch) =>
    set((state) => ({
      tickers: { ...state.tickers, [pair]: { ...(state.tickers[pair] || { pair }), ...patch } },
    })),
  setOrderbook: (orderbook) => set({ orderbook }),
  addTrade: (trade) => set((state) => ({ trades: [trade, ...state.trades].slice(0, 200) })),
  addFill: (fill) => set((state) => ({ fills: [fill, ...state.fills].slice(0, 50) })),
  clearFills: () => set({ fills: [] }),
}));
