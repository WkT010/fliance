import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
import { Badge } from '@/components/common/Badge';
import { formatPrice, formatQty, formatChangePct, changeColorClass, cls } from '@/utils/format';
import { getBalances } from '@/api/wallet';
import type { Balance } from '@/types';

export function TradingPage() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const [pair, setPair] = useState(params.get('pair') || 'BTC/USDT');
  const [leftTab, setLeftTab] = useState<'orderbook' | 'depth'>('orderbook');
  const [balances, setBalances] = useState<Balance[]>([]);
  useMarket(pair);
  const ticker = useMarketStore((s) => s.tickers[pair]);

  useEffect(() => {
    let cancelled = false;
    getBalances()
      .then((b) => { if (!cancelled) setBalances(b); })
      .catch(() => { /* ignore, fallback to default max notional */ });
    return () => { cancelled = true; };
  }, []);

  const maxNotional = useMemo(() => {
    const usdt = balances.find((b) => b.asset === 'USDT');
    return usdt ? Number(usdt.available) || 0 : 10000;
  }, [balances]);

  const changePair = (p: string) => { setPair(p); setParams({ pair: p }); };

  return (
    <Layout>
      <div className="flex flex-col gap-3 p-3 pb-6">
        {/* Price header - sticky so it stays visible while scrolling */}
        <div className="sticky top-0 z-30 -mx-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-nexa-700/70 bg-nexa-900/85 px-4 py-3 shadow-lg shadow-black/30 backdrop-blur-xl">
          <div className="flex flex-wrap items-center gap-4">
            <Select
              className="w-40"
              value={pair}
              onChange={(e) => changePair(e.target.value)}
              options={SUPPORTED_PAIRS.map((p) => ({ value: p, label: p }))}
            />
            <div className="flex items-center gap-3">
              <div className="font-mono text-2xl font-bold tabular-nums">
                <span className={changeColorClass(ticker?.change_pct_24h)}>
                  {formatPrice(ticker?.last, 2)}
                </span>
              </div>
              <Badge color={Number(ticker?.change_pct_24h ?? 0) >= 0 ? 'up' : 'down'}>
                {formatChangePct(ticker?.change_pct_24h)}
              </Badge>
              <span className={cls('font-mono text-sm font-medium', changeColorClass(ticker?.change_24h))}>
                {ticker?.change_24h ? `${Number(ticker.change_24h) >= 0 ? '+' : ''}${formatPrice(ticker.change_24h, 2)}` : '—'}
              </span>
            </div>
            <div className="hidden flex-wrap items-center gap-x-5 gap-y-1 text-xs md:flex">
              <div>
                <div className="text-nexa-500">{t('trading.high')}</div>
                <div className="font-mono text-nexa-100">{formatPrice(ticker?.high_24h, 2) || '—'}</div>
              </div>
              <div>
                <div className="text-nexa-500">{t('trading.low')}</div>
                <div className="font-mono text-nexa-100">{formatPrice(ticker?.low_24h, 2) || '—'}</div>
              </div>
              <div>
                <div className="text-nexa-500">{t('trading.vol')}</div>
                <div className="font-mono text-nexa-100">
                  {ticker?.volume_24h ? formatQty(ticker.volume_24h, 0) : '—'}
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Main grid */}
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-12">
          {/* Left column */}
          <div className="order-3 flex flex-col gap-3 lg:order-1 lg:col-span-3">
            <div className="min-h-[420px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <div className="flex border-b border-nexa-700/70 bg-nexa-900/40">
                <button
                  className={cls(
                    'flex-1 px-3 py-2 text-xs font-semibold transition-colors',
                    leftTab === 'orderbook'
                      ? 'border-b-2 border-accent text-nexa-100'
                      : 'text-nexa-400 hover:text-nexa-200'
                  )}
                  onClick={() => setLeftTab('orderbook')}
                >
                  {t('trading.orderBook')}
                </button>
                <button
                  className={cls(
                    'flex-1 px-3 py-2 text-xs font-semibold transition-colors',
                    leftTab === 'depth'
                      ? 'border-b-2 border-accent text-nexa-100'
                      : 'text-nexa-400 hover:text-nexa-200'
                  )}
                  onClick={() => setLeftTab('depth')}
                >
                  {t('trading.depth')}
                </button>
              </div>
              <div className="min-h-[360px]">
                {leftTab === 'orderbook' ? <OrderbookPanel pair={pair} compact /> : <DepthChart pair={pair} compact />}
              </div>
            </div>
            <div className="min-h-[420px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <RecentTrades pair={pair} />
            </div>
          </div>

          {/* Center column */}
          <div className="order-1 flex flex-col gap-3 lg:order-2 lg:col-span-6">
            <div className="min-h-[520px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <ChartPanel pair={pair} />
            </div>
            <div className="min-h-[240px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <OrdersPanel pair={pair} />
            </div>
          </div>

          {/* Right column */}
          <div className="order-2 lg:order-3 lg:col-span-3">
            <OrderForm pair={pair} maxNotional={maxNotional} markPrice={ticker?.last} />
          </div>
        </div>
      </div>
    </Layout>
  );
}
