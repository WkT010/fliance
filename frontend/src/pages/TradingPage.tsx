import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Layout } from '@/components/Layout';
import { ChartPanel } from '@/components/trading/ChartPanel';
import { OrderbookPanel } from '@/components/trading/OrderbookPanel';
import { DepthChart } from '@/components/trading/DepthChart';
import { RecentTrades } from '@/components/trading/RecentTrades';
import { OrderForm } from '@/components/trading/OrderForm';
import { OrdersPanel } from '@/components/trading/OrdersPanel';
import { useMarket } from '@/hooks/useMarket';
import { useMarketStore } from '@/store/marketStore';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { Select } from '@/components/common/Select';
import { formatPrice, changeColorClass } from '@/utils/format';

export function TradingPage() {
  const [params, setParams] = useSearchParams();
  const [pair, setPair] = useState(params.get('pair') || 'BTC/USDT');
  const [leftTab, setLeftTab] = useState<'orderbook' | 'depth'>('orderbook');
  useMarket(pair);
  const ticker = useMarketStore((s) => s.tickers[pair]);

  const changePair = (p: string) => { setPair(p); setParams({ pair: p }); };

  return (
    <Layout>
      <div className="flex h-full flex-col gap-2 p-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Select className="w-40" value={pair} onChange={(e) => changePair(e.target.value)} options={SUPPORTED_PAIRS.map((p) => ({ value: p, label: p }))} />
            <div className="flex items-center gap-4">
              <span className="text-xl font-semibold font-mono tabular-nums"><span className={changeColorClass(ticker?.change_pct_24h)}>{formatPrice(ticker?.last, 2)}</span></span>
              <span className={`text-sm font-medium ${changeColorClass(ticker?.change_pct_24h)}`}>
                {ticker?.change_24h ? `${Number(ticker.change_24h) >= 0 ? '+' : ''}${formatPrice(ticker.change_24h, 2)}` : '--'} {ticker?.change_pct_24h ? `(${Number(ticker.change_pct_24h) >= 0 ? '+' : ''}${Number(ticker.change_pct_24h).toFixed(2)}%)` : ''}
              </span>
              <div className="hidden sm:flex items-center gap-4 text-xs text-nexa-400">
                <div><span className="block text-nexa-500">High</span><span className="font-mono">{formatPrice(ticker?.high_24h, 2) || '--'}</span></div>
                <div><span className="block text-nexa-500">Low</span><span className="font-mono">{formatPrice(ticker?.low_24h, 2) || '--'}</span></div>
                <div><span className="block text-nexa-500">Vol</span><span className="font-mono">{ticker?.volume_24h ? Number(ticker.volume_24h).toLocaleString('en-US', { maximumFractionDigits: 0 }) : '--'}</span></div>
              </div>
            </div>
          </div>
        </div>
        <div className="grid flex-1 grid-cols-1 gap-2 lg:grid-cols-12">
          <div className="lg:col-span-3 flex flex-col gap-2">
            <div className="h-1/2 flex flex-col">
              <div className="flex border-b border-nexa-700">
                <button className={`flex-1 px-3 py-1.5 text-xs font-medium transition-colors ${leftTab === 'orderbook' ? 'border-b-2 border-accent text-nexa-100' : 'text-nexa-400 hover:text-nexa-300'}`} onClick={() => setLeftTab('orderbook')}>Order Book</button>
                <button className={`flex-1 px-3 py-1.5 text-xs font-medium transition-colors ${leftTab === 'depth' ? 'border-b-2 border-accent text-nexa-100' : 'text-nexa-400 hover:text-nexa-300'}`} onClick={() => setLeftTab('depth')}>Depth</button>
              </div>
              <div className="flex-1 min-h-0">{leftTab === 'orderbook' ? <OrderbookPanel pair={pair} compact /> : <DepthChart pair={pair} compact />}</div>
            </div>
            <div className="h-1/2"><RecentTrades pair={pair} /></div>
          </div>
          <div className="lg:col-span-6 flex flex-col gap-2">
            <div className="flex-1"><ChartPanel pair={pair} /></div>
            <div className="h-48"><OrdersPanel pair={pair} /></div>
          </div>
          <div className="lg:col-span-3"><OrderForm pair={pair} /></div>
        </div>
      </div>
    </Layout>
  );
}