import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Badge } from '@/components/common/Badge';
import { get24hTickers, getPriceComparison } from '@/api/market';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatQty, changeColorClass, cls } from '@/utils/format';
import type { Ticker, PriceComparison } from '@/types';

export function MarketsPage() {
  const { t } = useTranslation();
  const [tickers, setTickers] = useState<Ticker[]>([]);
  const [compare, setCompare] = useState<Record<string, PriceComparison>>({});

  const load = async () => {
    const res = await get24hTickers();
    setTickers(res.tickers);
    // compare internal vs uniswap for ETH pairs
    const pairs = ['ETH/USDC', 'ETH/USDT'];
    for (const p of pairs) {
      try {
        const c = await getPriceComparison(p);
        setCompare((prev) => ({ ...prev, [p]: c }));
      } catch { /* ignore */ }
    }
  };

  usePolling(load, 5000);

  const { gainers, losers } = useMemo(() => {
    const sorted = [...tickers].filter((tk) => tk.change_pct_24h).sort((a, b) => Number(b.change_pct_24h) - Number(a.change_pct_24h));
    return { gainers: sorted.slice(0, 3), losers: sorted.slice(-3).reverse() };
  }, [tickers]);

  const MoverItem = ({ ticker }: { ticker: Ticker }) => (
    <Link
      to={`/?pair=${encodeURIComponent(ticker.pair)}`}
      className="flex items-center justify-between rounded border border-nexa-700/50 bg-nexa-900/50 p-2 transition-colors hover:bg-nexa-800/50"
    >
      <div>
        <div className="text-sm font-medium text-nexa-100">{ticker.pair}</div>
        <div className="font-mono text-xs text-nexa-300">{formatPrice(ticker.last, 2)}</div>
      </div>
      <div className={cls('font-mono text-sm font-medium', changeColorClass(ticker.change_pct_24h))}>
        {`${Number(ticker.change_pct_24h) > 0 ? '+' : ''}${formatPrice(ticker.change_pct_24h, 2)}%`}
      </div>
    </Link>
  );

  return (
    <Layout>
      <div className="grid h-full grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        <Card className="lg:col-span-2" title={t('markets.markets24h')}>
          <div className="overflow-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400">
                <tr>
                  <th className="px-4 py-2">{t('markets.pair')}</th>
                  <th className="px-4 py-2">{t('markets.lastPrice')}</th>
                  <th className="px-4 py-2">{t('markets.change')}</th>
                  <th className="px-4 py-2">{t('markets.volume')}</th>
                  <th className="px-4 py-2"></th>
                </tr>
              </thead>
              <tbody>
                {tickers.map((tk) => (
                  <tr key={tk.pair} className="border-b border-nexa-700/50 hover:bg-nexa-800/50">
                    <td className="px-4 py-3 font-medium text-nexa-100">{tk.pair}</td>
                    <td className="px-4 py-3 font-mono">{formatPrice(tk.last, 2)}</td>
                    <td className={cls('px-4 py-3 font-mono', changeColorClass(tk.change_pct_24h))}>
                      {tk.change_pct_24h ? `${parseFloat(tk.change_pct_24h) > 0 ? '+' : ''}${formatPrice(tk.change_pct_24h, 2)}%` : '--'}
                    </td>
                    <td className="px-4 py-3 font-mono text-nexa-300">{formatQty(tk.volume_24h, 2)}</td>
                    <td className="px-4 py-3"><Link to={`/?pair=${encodeURIComponent(tk.pair)}`}><Badge color="accent">{t('markets.trade')}</Badge></Link></td>
                  </tr>
                ))}
                {tickers.length === 0 && <tr><td colSpan={5} className="py-8 text-center text-nexa-500">{t('markets.loadingMarkets')}</td></tr>}
              </tbody>
            </table>
          </div>
        </Card>
        <div className="flex flex-col gap-4">
          <Card title={t('markets.onChainMonitor')}>
            <div className="space-y-3 p-2">
              <p className="text-xs text-nexa-400">{t('markets.internalVsUniswap')}</p>
              {Object.entries(compare).map(([pair, c]) => (
                <div key={pair} className="rounded border border-nexa-700 bg-nexa-900 p-3">
                  <div className="mb-2 text-sm font-medium text-nexa-100">{pair}</div>
                  <div className="flex justify-between text-xs">
                    <span className="text-nexa-400">{t('markets.internal')}</span>
                    <span className="font-mono text-nexa-100">{c.internal.available ? formatPrice(c.internal.last, 4) : t('common.notAvailable')}</span>
                  </div>
                  <div className="flex justify-between text-xs">
                    <span className="text-nexa-400">{t('markets.uniswap')}</span>
                    <span className="font-mono text-nexa-100">{c.uniswap.available ? formatPrice(c.uniswap.last, 4) : c.uniswap.error || t('common.notAvailable')}</span>
                  </div>
                </div>
              ))}
            </div>
          </Card>
          <Card title={t('markets.topMovers')}>
            <div className="space-y-4 p-2">
              <div>
                <div className="mb-2 text-xs font-medium uppercase tracking-wide text-up">{t('markets.topGainers')}</div>
                <div className="space-y-2">
                  {gainers.length ? gainers.map((tk) => <MoverItem key={tk.pair} ticker={tk} />) : <div className="text-sm text-nexa-500">{t('markets.noData')}</div>}
                </div>
              </div>
              <div>
                <div className="mb-2 text-xs font-medium uppercase tracking-wide text-down">{t('markets.topLosers')}</div>
                <div className="space-y-2">
                  {losers.length ? losers.map((tk) => <MoverItem key={tk.pair} ticker={tk} />) : <div className="text-sm text-nexa-500">{t('markets.noData')}</div>}
                </div>
              </div>
            </div>
          </Card>
        </div>
      </div>
    </Layout>
  );
}
