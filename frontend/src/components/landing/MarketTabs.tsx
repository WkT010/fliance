import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { formatPrice, formatChangePct, formatUsd, cls } from '@/utils/format';
import { CoinIcon, coinName } from './coinMeta';
import type { LandingMarketData } from './useLandingMarket';
import type { Ticker } from '@/types';

type Tab = 'hot' | 'gainers' | 'amm';

function toNum(v: string | undefined): number {
  if (!v) return 0;
  const n = parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

function ChangePill({ value }: { value: string | undefined }) {
  const n = toNum(value);
  return (
    <span
      className={cls(
        'inline-flex min-w-[88px] justify-center rounded px-2 py-1 text-sm font-medium',
        n > 0 && 'bg-gain-bg text-gain',
        n < 0 && 'bg-loss-bg text-loss',
        n === 0 && 'bg-container text-sec'
      )}
    >
      {formatChangePct(value)}
    </span>
  );
}

function TickerRow({ tk }: { tk: Ticker }) {
  const [base, quote] = tk.pair.split('/');
  return (
    <Link
      to="/markets"
      className="grid grid-cols-[1.6fr_1fr_1fr] items-center gap-2 rounded-lg px-4 py-4 transition-colors hover:bg-container md:grid-cols-[2.2fr_1fr_1fr_1.2fr]"
    >
      <div className="flex items-center gap-3">
        <CoinIcon symbol={base} />
        <div>
          <div className="text-sm font-semibold text-pri">
            {base}
            <span className="ml-1 text-xs font-normal text-third">/{quote}</span>
          </div>
          <div className="mt-0.5 hidden text-xs text-third sm:block">{coinName(base)}</div>
        </div>
      </div>
      <div className="text-right font-mono text-sm font-medium text-pri">{formatPrice(tk.last)}</div>
      <div className="text-right">
        <ChangePill value={tk.change_pct_24h} />
      </div>
      <div className="hidden text-right font-mono text-sm text-sec md:block">
        {formatUsd(toNum(tk.quote_volume_24h) || toNum(tk.volume_24h))}
      </div>
    </Link>
  );
}

export function MarketTabs({ market }: { market: LandingMarketData }) {
  const { t } = useTranslation();
  const { tickers, pools, loading } = market;
  const [tab, setTab] = useState<Tab>('hot');

  const tabs: Array<{ id: Tab; labelKey: string }> = [
    { id: 'hot', labelKey: 'landing.tabHot' },
    { id: 'gainers', labelKey: 'landing.tabGainers' },
    { id: 'amm', labelKey: 'landing.tabAmm' },
  ];

  const rows = [...tickers];
  if (tab === 'hot') {
    rows.sort((a, b) => toNum(b.quote_volume_24h) - toNum(a.quote_volume_24h));
  } else {
    rows.sort((a, b) => toNum(b.change_pct_24h) - toNum(a.change_pct_24h));
  }

  const lastPrice = new Map<string, number>();
  for (const tk of tickers) lastPrice.set(tk.pair, toNum(tk.last));

  const poolTvl = (reserve0: string, reserve1: string, pair: string): number => {
    const r1 = toNum(reserve1);
    const r0 = toNum(reserve0);
    const price = lastPrice.get(pair) ?? 0;
    return r1 + (price > 0 ? r0 * price : r1);
  };

  return (
    <section id="markets" className="bg-bg1">
      <div className="mx-auto max-w-6xl px-6 py-16 lg:py-24">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="flex items-center gap-3 text-2xl font-semibold text-pri">
              {t('landing.marketsTitle')}
              <span className="inline-flex items-center gap-1.5 rounded bg-gain-bg px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wider text-gain">
                <span className="h-1.5 w-1.5 animate-pulse-soft rounded-full bg-gain" />
                {t('landing.liveBadge')}
              </span>
            </h2>
            <p className="mt-2 text-sm text-sec">{t('landing.marketsSubtitle')}</p>
          </div>
        </div>

        {/* Tabs */}
        <div className="mt-8 flex items-center gap-8 border-b border-line">
          {tabs.map((tb) => (
            <button
              key={tb.id}
              onClick={() => setTab(tb.id)}
              className={cls(
                '-mb-px border-b-2 pb-3 text-base transition-colors',
                tab === tb.id
                  ? 'border-cta font-semibold text-pri'
                  : 'border-transparent font-normal text-sec hover:text-pri'
              )}
            >
              {t(tb.labelKey)}
            </button>
          ))}
        </div>

        {/* Column headers (labels adapt to the active tab) */}
        <div className="mt-2 grid grid-cols-[1.6fr_1fr_1fr] gap-2 px-4 py-2 text-xs font-normal text-third md:grid-cols-[2.2fr_1fr_1fr_1.2fr]">
          <div>{t('markets.pair')}</div>
          <div className="text-right">
            {tab === 'amm' ? t('amm.feeRate') : t('markets.lastPrice')}
          </div>
          <div className="text-right">
            {tab === 'amm' ? t('trading.status') : t('markets.change')}
          </div>
          <div className="hidden text-right md:block">
            {tab === 'amm' ? t('landing.ammTvl') : t('markets.volume')}
          </div>
        </div>

        <div className="space-y-1">
          {tab !== 'amm' &&
            rows.map((tk) => <TickerRow key={tk.pair} tk={tk} />)}

          {tab === 'amm' &&
            pools.map((p) => {
              const [base, quote] = p.pair.split('/');
              return (
                <Link
                  key={p.id}
                  to="/amm"
                  className="grid grid-cols-[1.6fr_1fr_1fr] items-center gap-2 rounded-lg px-4 py-4 transition-colors hover:bg-container md:grid-cols-[2.2fr_1fr_1fr_1.2fr]"
                >
                  <div className="flex items-center gap-3">
                    <div className="flex -space-x-2">
                      <CoinIcon symbol={base} />
                      <CoinIcon symbol={quote} />
                    </div>
                    <div>
                      <div className="text-sm font-semibold text-pri">
                        {base}
                        <span className="ml-1 text-xs font-normal text-third">/{quote}</span>
                      </div>
                      <div className="mt-0.5 text-xs text-third">{t('landing.ammPoolDesc')}</div>
                    </div>
                  </div>
                  <div className="text-right font-mono text-sm text-sec">
                    {(toNum(p.fee_rate) * 100).toFixed(1)}%
                  </div>
                  <div className="text-right">
                    <span className="inline-flex min-w-[88px] justify-center rounded bg-cta/10 px-2 py-1 text-sm font-medium text-cta">
                      {p.status === 'active' ? t('settings.active') : p.status}
                    </span>
                  </div>
                  <div className="hidden text-right font-mono text-sm font-medium text-pri md:block">
                    {formatUsd(poolTvl(p.reserve0, p.reserve1, p.pair))}
                  </div>
                </Link>
              );
            })}

          {loading && rows.length === 0 && pools.length === 0 && (
            <div className="px-4 py-12 text-center text-sm text-third">{t('common.loading')}</div>
          )}
        </div>
      </div>
    </section>
  );
}
