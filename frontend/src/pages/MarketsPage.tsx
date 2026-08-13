import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Badge } from '@/components/common/Badge';
import { EmptyState } from '@/components/common/EmptyState';
import { get24hTickers, getPriceComparison } from '@/api/market';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatQty, formatPct, formatChangePct, changeColorClass, cls } from '@/utils/format';
import type { Ticker, PriceComparison } from '@/types';

interface TickerWithMeta extends Ticker {
  // Numeric helpers, lazily computed.
  _last: number;
  _change: number;
  _vol: number;
}

const TRACKED_PAIRS = ['BTC/USDT', 'ETH/USDT', 'SOL/USDT', 'BNB/USDT', 'ADA/USDT', 'ETH/USDC'];

export function MarketsPage() {
  const { t } = useTranslation();
  const [tickers, setTickers] = useState<Ticker[]>([]);
  const [compare, setCompare] = useState<Record<string, PriceComparison>>({});

  const load = async () => {
    try {
      const res = await get24hTickers();
      setTickers(res.tickers);
    } catch { /* ignore */ }

    for (const p of TRACKED_PAIRS) {
      try {
        const c = await getPriceComparison(p);
        setCompare((prev) => ({ ...prev, [p]: c }));
      } catch { /* ignore */ }
    }
  };

  usePolling(load, 5000);

  const enriched: TickerWithMeta[] = useMemo(
    () => tickers.map((tk) => ({
      ...tk,
      _last: parseFloat(tk.last || '0') || 0,
      _change: parseFloat(tk.change_pct_24h || '0') || 0,
      _vol: parseFloat(tk.volume_24h || '0') || 0,
    })),
    [tickers]
  );

  const { gainers, losers } = useMemo(() => {
    const sorted = [...enriched].sort((a, b) => b._change - a._change);
    return { gainers: sorted.slice(0, 3), losers: sorted.slice(-3).reverse() };
  }, [enriched]);

  const MoverItem = ({ ticker }: { ticker: TickerWithMeta }) => (
    <Link
      to={`/?pair=${encodeURIComponent(ticker.pair)}`}
      className="flex items-center justify-between rounded-lg border border-nexa-700/50 bg-nexa-900/40 p-2.5 transition-all hover:border-nexa-600 hover:bg-nexa-800/60"
    >
      <div>
        <div className="text-sm font-medium text-nexa-100">{ticker.pair}</div>
        <div className="font-mono text-xs text-nexa-300">{formatPrice(ticker.last, 2)}</div>
      </div>
      <div className={cls('rounded-md px-2 py-0.5 font-mono text-xs font-semibold', changeColorClass(ticker.change_pct_24h), ticker._change > 0 ? 'bg-up/10' : ticker._change < 0 ? 'bg-down/10' : 'bg-nexa-800')}>
        {formatChangePct(ticker.change_pct_24h)}
      </div>
    </Link>
  );

  return (
    <Layout>
      <div className="grid grid-cols-1 gap-4 p-4 pb-8 lg:grid-cols-3">
        <Card
          className="lg:col-span-2"
          title={t('markets.markets24h')}
          extra={<span className="flex items-center gap-1.5 text-up"><span className="h-1.5 w-1.5 rounded-full bg-up animate-pulse-soft" />Live</span>}
        >
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400">
                <tr className="border-b border-nexa-700/50">
                  <th className="px-4 py-3 font-medium">{t('markets.pair')}</th>
                  <th className="px-4 py-3 font-medium text-right">{t('markets.lastPrice')}</th>
                  <th className="px-4 py-3 font-medium text-right">{t('markets.change')}</th>
                  <th className="px-4 py-3 font-medium text-right">{t('markets.volume')}</th>
                  <th className="px-4 py-3 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody>
                {enriched.map((tk) => (
                  <tr key={tk.pair} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-accent/30 to-cta-deep/30 text-xs font-bold text-accent">
                          {tk.pair.split('/')[0].slice(0, 1)}
                        </span>
                        <span className="font-medium text-nexa-100">{tk.pair}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-nexa-100">{formatPrice(tk.last, 2)}</td>
                    <td className={cls('px-4 py-3 text-right font-mono font-semibold', changeColorClass(tk.change_pct_24h))}>
                      {tk.change_pct_24h ? formatChangePct(tk.change_pct_24h) : '—'}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-nexa-300">{formatQty(tk.volume_24h, 2)}</td>
                    <td className="px-4 py-3 text-right">
                      <Link to={`/trading?pair=${encodeURIComponent(tk.pair)}`}>
                        <Badge color="accent">{t('markets.trade')}</Badge>
                      </Link>
                    </td>
                  </tr>
                ))}
                {enriched.length === 0 && (
                  <tr>
                    <td colSpan={5}>
                      <EmptyState
                        title={t('markets.loadingMarkets')}
                        description="—"
                        compact
                      />
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Card>

        <div className="flex flex-col gap-4">
          <Card title={t('markets.topMovers')}>
            <div className="space-y-4 p-2">
              <div>
                <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-up">
                  <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
                    <path d="M5 17l7-7 7 7M5 7h14" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  {t('markets.topGainers')}
                </div>
                <div className="space-y-1.5">
                  {gainers.length ? gainers.map((tk) => <MoverItem key={tk.pair} ticker={tk} />) : (
                    <div className="text-sm text-nexa-500">{t('markets.noData')}</div>
                  )}
                </div>
              </div>
              <div>
                <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-down">
                  <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
                    <path d="M5 7l7 7 7-7M5 17h14" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  {t('markets.topLosers')}
                </div>
                <div className="space-y-1.5">
                  {losers.length ? losers.map((tk) => <MoverItem key={tk.pair} ticker={tk} />) : (
                    <div className="text-sm text-nexa-500">{t('markets.noData')}</div>
                  )}
                </div>
              </div>
            </div>
          </Card>

          <Card title={t('markets.onChainMonitor')}>
            <div className="space-y-2 p-3">
              <p className="text-xs text-nexa-500">{t('markets.internalVsUniswap')}</p>
              {Object.entries(compare).map(([pair, c]) => {
                const diffPct = (() => {
                  if (!c.internal.available || !c.uniswap.available) return null;
                  const i = parseFloat(c.internal.last || '0');
                  const u = parseFloat(c.uniswap.last || '0');
                  if (!Number.isFinite(i) || !Number.isFinite(u) || i === 0) return null;
                  return ((i - u) / u) * 100;
                })();
                return (
                  <div key={pair} className="rounded-lg border border-nexa-700/70 bg-nexa-900/40 p-3">
                    <div className="mb-2 flex items-center justify-between">
                      <span className="text-sm font-semibold text-nexa-100">{pair}</span>
                      {diffPct !== null && (
                        <Badge color={Math.abs(diffPct) < 0.5 ? 'success' : 'warning'}>
                          {formatPct(diffPct)}
                        </Badge>
                      )}
                    </div>
                    <div className="space-y-1 text-xs">
                      <div className="flex justify-between">
                        <span className="text-nexa-400">{t('markets.internal')}</span>
                        <span className="font-mono text-nexa-100">
                          {c.internal.available ? formatPrice(c.internal.last, 4) : '—'}
                        </span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-nexa-400">{t('markets.uniswap')}</span>
                        <span className="font-mono text-nexa-100">
                          {c.uniswap.available ? formatPrice(c.uniswap.last, 4) : (c.uniswap.error || '—')}
                        </span>
                      </div>
                    </div>
                  </div>
                );
              })}
              {Object.keys(compare).length === 0 && (
                <div className="text-xs text-nexa-500">{t('markets.noData')}</div>
              )}
            </div>
          </Card>
        </div>
      </div>
    </Layout>
  );
}
