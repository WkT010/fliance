import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { formatUsd } from '@/utils/format';
import { CountUp } from './CountUp';
import type { LandingMarketData } from './useLandingMarket';

export function Hero({ market }: { market: LandingMarketData }) {
  const { t } = useTranslation();
  const { tickers, totalQuoteVolume, ammTvl, loading } = market;

  const moneyStat = (n: number) => (
    <>
      <span className="text-brand">$</span>
      <CountUp value={n} format={(v) => formatUsd(v).replace('$', '')} />
    </>
  );

  return (
    <section className="relative overflow-hidden bg-bg1">
      {/* Ambient MetaMask-style gradient blobs (pure CSS transform animation) */}
      <div aria-hidden="true">
        <div
          className="hero-blob left-[-160px] top-[-140px] h-[480px] w-[480px]"
          style={{ background: 'radial-gradient(circle, rgba(45, 212, 191, 0.30) 0%, rgba(45, 212, 191, 0) 70%)' }}
        />
        <div
          className="hero-blob hero-blob-2 right-[-120px] top-[40px] h-[420px] w-[420px]"
          style={{ background: 'radial-gradient(circle, rgba(6, 182, 212, 0.22) 0%, rgba(6, 182, 212, 0) 70%)' }}
        />
        <div
          className="hero-blob hero-blob-3 bottom-[-160px] left-[35%] h-[440px] w-[440px]"
          style={{ background: 'radial-gradient(circle, rgba(20, 184, 166, 0.18) 0%, rgba(20, 184, 166, 0) 70%)' }}
        />
      </div>

      {/* Slow-drifting fine grid at the hero base (echo motif) */}
      <div aria-hidden="true" className="pointer-events-none absolute inset-x-0 bottom-0 h-40 overflow-hidden md:h-56">
        <div className="hero-grid" />
      </div>

      <div className="relative z-10 mx-auto max-w-6xl px-6 pb-16 pt-16 lg:pb-24 lg:pt-24">
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
            className="inline-flex h-12 w-[180px] items-center justify-center rounded-lg bg-cta text-base font-semibold text-bg1 transition-all duration-200 hover:-translate-y-0.5 hover:bg-cta-deep hover:shadow-[0_10px_30px_-6px_rgba(45,212,191,0.45)]"
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

        {/* Live stats strip (money stats count up from 0 on mount) */}
        <div className="mt-16 grid grid-cols-2 gap-x-6 gap-y-10 border-t border-line pt-10 md:grid-cols-4 md:gap-x-8">
          <div>
            <div className="text-2xl font-medium text-pri lg:text-[28px]">
              {loading ? '--' : moneyStat(totalQuoteVolume)}
            </div>
            <StatLabel labelKey="landing.stat24hVolume" />
          </div>
          <div>
            <div className="text-2xl font-medium text-pri lg:text-[28px]">{tickers.length || 5}+</div>
            <StatLabel labelKey="landing.statPairs" />
          </div>
          <div>
            <div className="text-2xl font-medium text-pri lg:text-[28px]">99.99%</div>
            <StatLabel labelKey="landing.statUptime" />
          </div>
          <div>
            <div className="text-2xl font-medium text-pri lg:text-[28px]">
              {loading ? '--' : moneyStat(ammTvl)}
            </div>
            <StatLabel labelKey="landing.statSecured" />
          </div>
        </div>
      </div>
    </section>
  );
}

function StatLabel({ labelKey }: { labelKey: string }) {
  const { t } = useTranslation();
  return (
    <div className="mt-2 flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-third">
      <span className="h-1.5 w-1.5 rounded-[1px] bg-cta" />
      {t(labelKey)}
    </div>
  );
}
