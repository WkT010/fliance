import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Layout } from '@/components/Layout';
import { ChartPanel } from '@/components/trading/ChartPanel';
import { OrderbookPanel } from '@/components/trading/OrderbookPanel';
import { RecentTrades } from '@/components/trading/RecentTrades';
import { OrderForm } from '@/components/trading/OrderForm';
import { OrdersPanel } from '@/components/trading/OrdersPanel';
import { useMarket } from '@/hooks/useMarket';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { Select } from '@/components/common/Select';

export function TradingPage() {
  const [params, setParams] = useSearchParams();
  const [pair, setPair] = useState(params.get('pair') || 'BTC/USDT');
  useMarket(pair);

  const changePair = (p: string) => {
    setPair(p);
    setParams({ pair: p });
  };

  return (
    <Layout>
      <div className="flex h-full flex-col gap-2 p-2">
        <div className="flex items-center gap-3">
          <Select
            className="w-40"
            value={pair}
            onChange={(e) => changePair(e.target.value)}
            options={SUPPORTED_PAIRS.map((p) => ({ value: p, label: p }))}
          />
        </div>
        <div className="grid flex-1 grid-cols-1 gap-2 lg:grid-cols-12">
          <div className="lg:col-span-3 flex flex-col gap-2">
            <div className="h-1/2"><OrderbookPanel pair={pair} /></div>
            <div className="h-1/2"><RecentTrades pair={pair} /></div>
          </div>
          <div className="lg:col-span-6 flex flex-col gap-2">
            <div className="flex-1"><ChartPanel pair={pair} /></div>
            <div className="h-48"><OrdersPanel pair={pair} /></div>
          </div>
          <div className="lg:col-span-3">
            <OrderForm pair={pair} />
          </div>
        </div>
      </div>
    </Layout>
  );
}
