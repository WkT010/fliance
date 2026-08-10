import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { formatUsd } from '@/utils/format';
import type { LandingMarketData } from './useLandingMarket';

interface StatItem {
  value: string;
  /** Splits the leading "$" into a brand-yellow accent, when present. */
  money?: boolean;
  labelKey: string;
}

export function Hero({ market }: { market: LandingMarketData }) {
  const { t } = useTranslation();
  const { tickers, totalQuoteVolume, ammTvl, loading } = market;

  const stats: StatItem[] = [
    { value: loading ? '--' : formatUsd(totalQuoteVolume), money: true, labelKey: 'landing.stat24hVolume' },
    { value: `${tickers.length || 5}+`, labelKey: 'landing.statPairs' },
    { value: '99.99%', labelKey: 'landing.statUptime' },
    { value: loading ? '--' : formatUsd(ammTvl), money: true, labelKey: 'landing.statSecured' },
  ];

  return (
    <section className="bg-bg1">
      <div className="mx-auto max-w-6xl px-6 pb-16 pt-16 lg:pb-24 lg:pt-24">
        {/* "No.1" badge */}
        <div className="flex items-center gap-3">
          <span className="flex h-9 min-w-9 items-center justify-center rounded bg-cta px-1 text-[11px] font-bold leading-none text-bg1">
            No.1
          </span>
          <span className="text-sm font-medium uppercase tracking-widest text-sec">
            {t('landing.heroBadgeText')}
          </span>
        </div>

        {/* H1 — the hero statement */}
        <h1 className="mt-8 max-w-4xl text-4xl font-semibold uppercase leading-[1.12] tracking-tight text-pri sm:text-5xl lg:text-[56px]">
          {t('landing.heroTitle')}
        </h1>

        <p className="mt-6 max-w-2xl text-lg leading-relaxed text-sec">
          {t('landing.heroSubtitle')}
        </p>

        {/* CTAs */}
        <div className="mt-10 flex flex-wrap items-center gap-6">
          <Link
            to="/register"
            className="inline-flex h-12 w-[180px] items-center justify-center rounded-lg bg-cta text-base font-semibold text-bg1 transition-colors hover:bg-brand"
          >
            {t('auth.register')}
          </Link>
          <Link
            to="/login"
            className="group inline-flex items-center gap-1.5 text-base font-medium text-pri transition-colors hover:text-cta"
          >
            {t('landing.startTrading')}
            <svg
              viewBox="0 0 24 24"
              fill="none"
              className="h-4 w-4 transition-transform group-hover:translate-x-0.5"
            >
              <path d="M9 6l6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </Link>
        </div>

        {/* Live stats strip */}
        <div className="mt-16 grid grid-cols-2 gap-x-6 gap-y-10 border-t border-line pt-10 md:grid-cols-4 md:gap-x-8">
          {stats.map((s) => (
            <div key={s.labelKey}>
              <div className="text-2xl font-medium text-pri lg:text-[28px]">
                {s.money && s.value.startsWith('$') ? (
                  <>
                    <span className="text-brand">$</span>
                    {s.value.slice(1)}
                  </>
                ) : (
                  s.value
                )}
              </div>
              <div className="mt-2 flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-third">
                <span className="h-1.5 w-1.5 rounded-[1px] bg-cta" />
                {t(s.labelKey)}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
