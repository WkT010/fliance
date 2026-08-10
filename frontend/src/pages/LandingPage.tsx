import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/store/authStore';
import { Button } from '@/components/common/Button';
import { Badge } from '@/components/common/Badge';
import { getTickers } from '@/api/market';
import { formatPrice, formatPct, changeColorClass, cls, formatUsd } from '@/utils/format';
import type { Ticker } from '@/types';

const FEATURES = [
  {
    key: 'spot',
    titleKey: 'landing.spot.title',
    descKey: 'landing.spot.desc',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <path d="M3 13l4-4 4 4 5-5 5 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
        <path d="M3 19h18" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
      </svg>
    ),
  },
  {
    key: 'futures',
    titleKey: 'landing.futures.title',
    descKey: 'landing.futures.desc',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <path d="M12 2v20M5 9l7-7 7 7M5 15l7 7 7-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
      </svg>
    ),
  },
  {
    key: 'amm',
    titleKey: 'landing.amm.title',
    descKey: 'landing.amm.desc',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2"/>
        <path d="M12 3a9 9 0 019 9h-9V3z" fill="currentColor" opacity="0.3"/>
        <path d="M12 3a9 9 0 019 9h-9V3z" stroke="currentColor" strokeWidth="2" strokeLinejoin="round"/>
      </svg>
    ),
  },
  {
    key: 'wallet',
    titleKey: 'landing.wallet.title',
    descKey: 'landing.wallet.desc',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <rect x="3" y="6" width="18" height="14" rx="2" stroke="currentColor" strokeWidth="2"/>
        <path d="M3 10h18M16 15h2" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
      </svg>
    ),
  },
];

const STATS = [
  { value: '5+', labelKey: 'landing.statPairs' },
  { value: '3', labelKey: 'landing.statChains' },
  { value: '<1ms', labelKey: 'landing.statLatency' },
  { value: '24/7', labelKey: 'landing.statAlwaysOn' },
];

export function LandingPage() {
  const { t } = useTranslation();
  const { user } = useAuthStore();
  const [tickers, setTickers] = useState<Ticker[]>([]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await getTickers();
        if (!cancelled) setTickers(res.tickers || []);
      } catch { /* ignore */ }
    };
    load();
    const id = setInterval(load, 5000);
    return () => { cancelled = true; clearInterval(id); };
  }, []);

  const featuredPairs = ['BTC/USDT', 'ETH/USDT', 'SOL/USDT', 'BNB/USDT', 'ADA/USDT'];

  return (
    <div className="min-h-screen bg-nexa-950 text-nexa-100">
      {/* ── Background decoration ── */}
      <div className="pointer-events-none fixed inset-0 overflow-hidden">
        <div className="absolute -top-40 left-1/4 h-96 w-96 rounded-full bg-accent/15 blur-[120px]" />
        <div className="absolute top-1/3 right-0 h-80 w-80 rounded-full bg-up/10 blur-[100px]" />
        <div
          className="absolute inset-0 opacity-[0.03]"
          style={{
            backgroundImage:
              'linear-gradient(to right, #fff 1px, transparent 1px), linear-gradient(to bottom, #fff 1px, transparent 1px)',
            backgroundSize: '48px 48px',
          }}
        />
      </div>

      {/* ── Nav bar ── */}
      <header className="sticky top-0 z-50 border-b border-nexa-800/60 bg-nexa-950/80 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6">
          <div className="flex items-center gap-2">
            <span className="flex h-8 w-8 items-center justify-center rounded-md bg-accent text-sm font-bold text-nexa-950 shadow-md shadow-accent/30">F</span>
            <span className="text-xl font-bold tracking-tight text-nexa-100">Fliance</span>
            <span className="hidden text-xs font-medium text-nexa-400 sm:inline">梵响</span>
          </div>
          <nav className="hidden items-center gap-8 text-sm text-nexa-300 md:flex">
            <a href="#features" className="transition-colors hover:text-nexa-100">{t('landing.navFeatures')}</a>
            <a href="#markets" className="transition-colors hover:text-nexa-100">{t('landing.navMarkets')}</a>
            <a href="#footer" className="transition-colors hover:text-nexa-100">{t('landing.navAbout')}</a>
          </nav>
          <div className="flex items-center gap-2">
            {user ? (
              <Link to="/">
                <Button size="sm">{t('landing.enterExchange')}</Button>
              </Link>
            ) : (
              <>
                <Link to="/login">
                  <Button variant="ghost" size="sm">{t('auth.signIn')}</Button>
                </Link>
                <Link to="/register">
                  <Button size="sm">{t('landing.getStarted')}</Button>
                </Link>
              </>
            )}
          </div>
        </div>
      </header>

      {/* ── Hero ── */}
      <section className="relative mx-auto max-w-7xl px-6 pt-24 pb-20 text-center">
        <div className="mx-auto mb-6 inline-flex items-center gap-2 rounded-full border border-nexa-700 bg-nexa-900/50 px-4 py-1.5 text-xs text-nexa-300 backdrop-blur-sm">
          <span className="relative flex h-1.5 w-1.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-up/60" />
            <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-up" />
          </span>
          {t('landing.heroBadge')}
        </div>
        <h1 className="mx-auto max-w-4xl text-5xl font-bold leading-tight tracking-tight sm:text-6xl lg:text-7xl">
          {t('landing.heroTitleA')}{' '}
          <span className="bg-gradient-to-r from-accent via-amber-300 to-amber-500 bg-clip-text text-transparent">
            {t('landing.heroTitleB')}
          </span>
        </h1>
        <p className="mx-auto mt-6 max-w-2xl text-lg text-nexa-400">
          {t('landing.heroSubtitle')}
        </p>
        <div className="mt-10 flex flex-col items-center justify-center gap-4 sm:flex-row">
          <Link to="/register">
            <Button size="lg" className="w-full shadow-xl shadow-accent/30 sm:w-auto" icon={
              <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
                <path d="M5 12h14M13 5l7 7-7 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            }>
              {t('landing.startTrading')}
            </Button>
          </Link>
          <Link to="/login">
            <Button variant="secondary" size="lg" className="w-full sm:w-auto">{t('auth.signIn')}</Button>
          </Link>
        </div>

        {/* Stats */}
        <div className="mx-auto mt-20 grid max-w-3xl grid-cols-2 gap-8 sm:grid-cols-4">
          {STATS.map((s) => (
            <div key={s.labelKey} className="rounded-xl border border-nexa-800/60 bg-nexa-900/40 p-3 backdrop-blur-sm">
              <div className="text-3xl font-bold text-accent">{s.value}</div>
              <div className="mt-1 text-xs uppercase tracking-wide text-nexa-500">{t(s.labelKey)}</div>
            </div>
          ))}
        </div>
      </section>

      {/* ── Live market ticker bar ── */}
      <section className="relative border-y border-nexa-800/60 bg-nexa-900/40 py-3">
        <div className="mx-auto max-w-7xl px-6">
          <div className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2">
            {tickers.length === 0 && (
              <span className="text-xs text-nexa-500">{t('common.loading')}</span>
            )}
            {tickers
              .filter((tk) => featuredPairs.includes(tk.pair))
              .slice(0, 5)
              .map((tk) => (
                <div key={tk.pair} className="flex items-center gap-2 text-sm">
                  <span className="font-semibold text-nexa-200">{tk.pair}</span>
                  <span className="font-mono text-nexa-100">{formatPrice(tk.last, 2)}</span>
                  <span className={cls('font-mono text-xs font-semibold', changeColorClass(tk.change_pct_24h))}>
                    {formatPct(tk.change_pct_24h)}
                  </span>
                </div>
              ))}
          </div>
        </div>
      </section>

      {/* ── Features ── */}
      <section id="features" className="relative mx-auto max-w-7xl px-6 py-24">
        <div className="mb-14 text-center">
          <Badge color="accent" className="mb-3">{t('landing.featuresBadge')}</Badge>
          <h2 className="text-3xl font-bold sm:text-4xl">{t('landing.featuresTitle')}</h2>
          <p className="mx-auto mt-4 max-w-2xl text-nexa-400">{t('landing.featuresSubtitle')}</p>
        </div>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
          {FEATURES.map((f) => (
            <div
              key={f.key}
              className="group relative overflow-hidden rounded-2xl border border-nexa-800 bg-nexa-900/40 p-6 backdrop-blur-sm transition-all hover:border-accent/30 hover:bg-nexa-900/80"
            >
              <div className="pointer-events-none absolute -right-12 -top-12 h-32 w-32 rounded-full bg-accent/10 opacity-0 blur-2xl transition-opacity group-hover:opacity-100" />
              <div className="relative mb-4 inline-flex rounded-xl bg-accent/10 p-3 text-accent transition-colors group-hover:bg-accent/20">
                {f.icon}
              </div>
              <h3 className="relative mb-2 text-lg font-semibold text-nexa-100">{t(f.titleKey)}</h3>
              <p className="relative text-sm leading-relaxed text-nexa-400">{t(f.descKey)}</p>
            </div>
          ))}
        </div>
      </section>

      {/* ── Live markets preview ── */}
      <section id="markets" className="relative mx-auto max-w-7xl px-6 py-20">
        <div className="mb-12 text-center">
          <Badge color="up" className="mb-3">
            <span className="mr-1.5 inline-block h-1.5 w-1.5 animate-pulse-soft rounded-full bg-up" />
            {t('landing.liveBadge')}
          </Badge>
          <h2 className="text-3xl font-bold sm:text-4xl">{t('landing.marketsTitle')}</h2>
          <p className="mx-auto mt-4 max-w-2xl text-nexa-400">{t('landing.marketsSubtitle')}</p>
        </div>
        <div className="mx-auto max-w-4xl overflow-hidden rounded-2xl border border-nexa-800 bg-nexa-900/40 shadow-2xl shadow-black/40 backdrop-blur-sm">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-nexa-800 bg-nexa-900/60 text-nexa-400">
              <tr>
                <th className="px-6 py-3 font-medium">{t('markets.pair')}</th>
                <th className="px-6 py-3 text-right font-medium">{t('markets.lastPrice')}</th>
                <th className="px-6 py-3 text-right font-medium">{t('markets.change')}</th>
                <th className="px-6 py-3 text-right font-medium">{t('markets.volume')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-nexa-800/60">
              {tickers
                .filter((tk) => featuredPairs.includes(tk.pair))
                .slice(0, 5)
                .map((tk) => (
                  <tr key={tk.pair} className="transition-colors hover:bg-nexa-900/40">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-accent/30 to-amber-700/30 text-xs font-bold text-accent">
                          {tk.pair.split('/')[0].slice(0, 1)}
                        </span>
                        <span className="font-semibold text-nexa-100">{tk.pair}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 text-right font-mono text-nexa-100">{formatPrice(tk.last, 2)}</td>
                    <td className={cls('px-6 py-4 text-right font-mono font-semibold', changeColorClass(tk.change_pct_24h))}>
                      {formatPct(tk.change_pct_24h)}
                    </td>
                    <td className="px-6 py-4 text-right font-mono text-nexa-400">{formatUsd(tk.volume_24h, 2)}</td>
                  </tr>
                ))}
              {tickers.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-6 py-12 text-center text-sm text-nexa-500">
                    {t('common.loading')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* ── CTA ── */}
      <section className="relative mx-auto max-w-7xl px-6 py-20">
        <div className="relative overflow-hidden rounded-3xl border border-nexa-800 bg-gradient-to-br from-nexa-900 to-nexa-950 p-12 text-center shadow-2xl shadow-black/40">
          <div className="pointer-events-none absolute -top-20 left-1/2 h-60 w-60 -translate-x-1/2 rounded-full bg-accent/15 blur-[80px]" />
          <div className="pointer-events-none absolute inset-0 opacity-[0.04]" style={{
            backgroundImage: 'linear-gradient(to right, #fff 1px, transparent 1px), linear-gradient(to bottom, #fff 1px, transparent 1px)',
            backgroundSize: '40px 40px',
          }} />
          <h2 className="relative text-3xl font-bold sm:text-4xl">{t('landing.ctaTitle')}</h2>
          <p className="relative mx-auto mt-4 max-w-xl text-nexa-400">{t('landing.ctaSubtitle')}</p>
          <div className="relative mt-8 flex flex-col items-center justify-center gap-4 sm:flex-row">
            <Link to="/register">
              <Button size="lg">{t('landing.ctaCreate')}</Button>
            </Link>
            <Link to="/login">
              <Button variant="ghost" size="lg">{t('landing.ctaHaveAccount')}</Button>
            </Link>
          </div>
        </div>
      </section>

      {/* ── Footer ── */}
      <footer id="footer" className="relative border-t border-nexa-800/60">
        <div className="mx-auto max-w-7xl px-6 py-12">
          <div className="flex flex-col items-center justify-between gap-6 sm:flex-row">
            <div className="flex items-center gap-2">
              <span className="flex h-7 w-7 items-center justify-center rounded-md bg-accent text-xs font-bold text-nexa-950">F</span>
              <span className="text-lg font-bold text-nexa-100">Fliance</span>
              <span className="text-xs text-nexa-500">梵响</span>
            </div>
            <nav className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-sm text-nexa-400">
              <Link to="/legal/terms" className="hover:text-nexa-100">{t('footer.terms')}</Link>
              <Link to="/legal/privacy" className="hover:text-nexa-100">{t('footer.privacy')}</Link>
              <Link to="/legal/risk" className="hover:text-nexa-100">{t('footer.risk')}</Link>
              <Link to="/legal/aml" className="hover:text-nexa-100">{t('footer.aml')}</Link>
              <Link to="/legal/cookies" className="hover:text-nexa-100">{t('footer.cookies')}</Link>
            </nav>
            <nav className="flex items-center gap-6 text-sm text-nexa-400">
              <Link to="/login" className="hover:text-nexa-100">{t('auth.signIn')}</Link>
              <Link to="/register" className="hover:text-nexa-100">{t('auth.register')}</Link>
            </nav>
          </div>
          <p className="mt-8 text-center text-xs text-nexa-500 sm:text-left">{t('footer.rights')}</p>
        </div>
      </footer>
    </div>
  );
}
