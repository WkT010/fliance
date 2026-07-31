import { Link } from 'react-router-dom';
import { useAuthStore } from '@/store/authStore';
import { Button } from '@/components/common/Button';

const FEATURES = [
  {
    title: 'Spot Trading',
    desc: 'Real-time order matching with sub-millisecond latency. Advanced order types, depth charts, and live trade streams.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <path d="M3 13l4-4 4 4 5-5 5 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
        <path d="M3 19h18" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
      </svg>
    ),
  },
  {
    title: 'Perpetual Futures',
    desc: 'Trade BTC, ETH, SOL and more with leverage. Auto-liquidation, take-profit / stop-loss, and funding rate mechanism.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <path d="M12 2v20M5 9l7-7 7 7M5 15l7 7 7-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
      </svg>
    ),
  },
  {
    title: 'AMM Liquidity',
    desc: 'Provide liquidity to constant-product pools and earn trading fees. Manage positions and track yields in real time.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2"/>
        <path d="M12 3a9 9 0 019 9h-9V3z" fill="currentColor" opacity="0.3"/>
        <path d="M12 3a9 9 0 019 9h-9V3z" stroke="currentColor" strokeWidth="2" strokeLinejoin="round"/>
      </svg>
    ),
  },
  {
    title: 'Secure Wallet',
    desc: 'Multi-chain wallet with on-chain settlement. Whitelisted withdrawals, daily limits, and full audit trail.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
        <rect x="3" y="6" width="18" height="14" rx="2" stroke="currentColor" strokeWidth="2"/>
        <path d="M3 10h18M16 15h2" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
      </svg>
    ),
  },
];

const STATS = [
  { value: '5+', label: 'Trading Pairs' },
  { value: '3', label: 'Blockchains' },
  { value: '<1ms', label: 'Match Latency' },
  { value: '24/7', label: 'Always On' },
];

const PAIRS = [
  { pair: 'BTC/USDT', tag: 'Spot' },
  { pair: 'ETH/USDT', tag: 'Spot' },
  { pair: 'SOL/USDT', tag: 'Spot' },
  { pair: 'BNB/USDT', tag: 'Spot' },
  { pair: 'ADA/USDT', tag: 'Spot' },
  { pair: 'BTC-PERP', tag: 'Futures' },
];

export function LandingPage() {
  const { user } = useAuthStore();

  return (
    <div className="min-h-screen bg-nexa-950 text-nexa-100">
      {/* ── Background decoration ── */}
      <div className="pointer-events-none fixed inset-0 overflow-hidden">
        <div className="absolute -top-40 left-1/4 h-96 w-96 rounded-full bg-accent/10 blur-[120px]" />
        <div className="absolute top-1/3 right-0 h-80 w-80 rounded-full bg-up/5 blur-[100px]" />
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
            <span className="text-2xl font-bold tracking-tight text-accent">NEXA</span>
            <span className="hidden text-xs font-medium text-nexa-400 sm:inline">EXCHANGE</span>
          </div>
          <nav className="hidden items-center gap-8 text-sm text-nexa-300 md:flex">
            <a href="#features" className="transition-colors hover:text-nexa-100">Features</a>
            <a href="#markets" className="transition-colors hover:text-nexa-100">Markets</a>
            <a href="#footer" className="transition-colors hover:text-nexa-100">About</a>
          </nav>
          <div className="flex items-center gap-3">
            {user ? (
              <Link to="/">
                <Button size="sm">Enter Exchange</Button>
              </Link>
            ) : (
              <>
                <Link to="/login">
                  <Button variant="ghost" size="sm">Sign In</Button>
                </Link>
                <Link to="/register">
                  <Button size="sm">Get Started</Button>
                </Link>
              </>
            )}
          </div>
        </div>
      </header>

      {/* ── Hero ── */}
      <section className="relative mx-auto max-w-7xl px-6 pt-24 pb-20 text-center">
        <div className="mx-auto mb-6 inline-flex items-center gap-2 rounded-full border border-nexa-700 bg-nexa-900/50 px-4 py-1.5 text-xs text-nexa-300">
          <span className="h-1.5 w-1.5 rounded-full bg-up" />
          Live · Multi-chain · Zero downtime
        </div>
        <h1 className="mx-auto max-w-4xl text-5xl font-bold leading-tight tracking-tight sm:text-6xl">
          Trade the future of{' '}
          <span className="bg-gradient-to-r from-accent to-amber-400 bg-clip-text text-transparent">
            decentralized finance
          </span>
        </h1>
        <p className="mx-auto mt-6 max-w-2xl text-lg text-nexa-400">
          A high-performance exchange engine combining spot, perpetual futures, and AMM
          liquidity pools — all in one secure, real-time platform.
        </p>
        <div className="mt-10 flex flex-col items-center justify-center gap-4 sm:flex-row">
          <Link to="/register">
            <Button size="lg" className="w-full sm:w-auto">Start Trading Now</Button>
          </Link>
          <Link to="/login">
            <Button variant="secondary" size="lg" className="w-full sm:w-auto">Sign In</Button>
          </Link>
        </div>

        {/* Stats */}
        <div className="mx-auto mt-20 grid max-w-3xl grid-cols-2 gap-8 sm:grid-cols-4">
          {STATS.map((s) => (
            <div key={s.label}>
              <div className="text-3xl font-bold text-accent">{s.value}</div>
              <div className="mt-1 text-sm text-nexa-400">{s.label}</div>
            </div>
          ))}
        </div>
      </section>

      {/* ── Features ── */}
      <section id="features" className="relative mx-auto max-w-7xl px-6 py-20">
        <div className="mb-14 text-center">
          <h2 className="text-3xl font-bold sm:text-4xl">Everything you need to trade</h2>
          <p className="mx-auto mt-4 max-w-2xl text-nexa-400">
            From lightning-fast spot matching to leveraged futures and automated market making —
            NEXA covers the full trading lifecycle.
          </p>
        </div>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
          {FEATURES.map((f) => (
            <div
              key={f.title}
              className="group rounded-2xl border border-nexa-800 bg-nexa-900/40 p-6 transition-all hover:border-accent/30 hover:bg-nexa-900/80"
            >
              <div className="mb-4 inline-flex rounded-xl bg-accent/10 p-3 text-accent transition-colors group-hover:bg-accent/20">
                {f.icon}
              </div>
              <h3 className="mb-2 text-lg font-semibold text-nexa-100">{f.title}</h3>
              <p className="text-sm leading-relaxed text-nexa-400">{f.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* ── Markets preview ── */}
      <section id="markets" className="relative mx-auto max-w-7xl px-6 py-20">
        <div className="mb-14 text-center">
          <h2 className="text-3xl font-bold sm:text-4xl">Markets</h2>
          <p className="mx-auto mt-4 max-w-2xl text-nexa-400">
            Trade top digital assets across spot and derivatives markets.
          </p>
        </div>
        <div className="mx-auto max-w-3xl overflow-hidden rounded-2xl border border-nexa-800">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-nexa-800 bg-nexa-900/60 text-nexa-400">
              <tr>
                <th className="px-6 py-3 font-medium">Pair</th>
                <th className="px-6 py-3 font-medium">Market</th>
                <th className="px-6 py-3 text-right font-medium">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-nexa-800">
              {PAIRS.map((p) => (
                <tr key={p.pair} className="transition-colors hover:bg-nexa-900/40">
                  <td className="px-6 py-4 font-medium text-nexa-100">{p.pair}</td>
                  <td className="px-6 py-4 text-nexa-400">{p.tag}</td>
                  <td className="px-6 py-4 text-right">
                    <span className="inline-flex items-center gap-1.5 rounded-full bg-up/10 px-2.5 py-0.5 text-xs font-medium text-up">
                      <span className="h-1.5 w-1.5 rounded-full bg-up" />
                      Active
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* ── CTA ── */}
      <section className="relative mx-auto max-w-7xl px-6 py-20">
        <div className="overflow-hidden rounded-3xl border border-nexa-800 bg-gradient-to-br from-nexa-900 to-nexa-950 p-12 text-center">
          <div className="pointer-events-none absolute -top-20 left-1/2 h-60 w-60 -translate-x-1/2 rounded-full bg-accent/10 blur-[80px]" />
          <h2 className="relative text-3xl font-bold sm:text-4xl">Ready to start trading?</h2>
          <p className="relative mx-auto mt-4 max-w-xl text-nexa-400">
            Create your account in seconds and access the full NEXA trading suite.
          </p>
          <div className="relative mt-8 flex flex-col items-center justify-center gap-4 sm:flex-row">
            <Link to="/register">
              <Button size="lg">Create Free Account</Button>
            </Link>
            <Link to="/login">
              <Button variant="ghost" size="lg">I already have an account</Button>
            </Link>
          </div>
        </div>
      </section>

      {/* ── Footer ── */}
      <footer id="footer" className="relative border-t border-nexa-800/60">
        <div className="mx-auto max-w-7xl px-6 py-12">
          <div className="flex flex-col items-center justify-between gap-6 sm:flex-row">
            <div className="flex items-center gap-2">
              <span className="text-xl font-bold text-accent">NEXA</span>
              <span className="text-xs text-nexa-400">EXCHANGE</span>
            </div>
            <nav className="flex items-center gap-6 text-sm text-nexa-400">
              <Link to="/legal" className="hover:text-nexa-100">Legal</Link>
              <Link to="/login" className="hover:text-nexa-100">Sign In</Link>
              <Link to="/register" className="hover:text-nexa-100">Register</Link>
            </nav>
          </div>
          <p className="mt-8 text-center text-xs text-nexa-500 sm:text-left">
            © {new Date().getFullYear()} NEXA Exchange. All rights reserved.
          </p>
        </div>
      </footer>
    </div>
  );
}
