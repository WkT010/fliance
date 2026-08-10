import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/store/authStore';
import { cls } from '@/utils/format';
import { LANGS, setLanguage } from '@/i18n';

interface NavItem {
  to: string;
  label: string;
  adminOnly?: boolean;
  /** Hidden from the public (logged-out) navigation. */
  authOnly?: boolean;
}

export function Header() {
  const { t, i18n } = useTranslation();
  const { user, isAdmin, logout } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const drawerRef = useRef<HTMLDivElement>(null);
  const userMenuRef = useRef<HTMLDivElement>(null);

  // Close the mobile drawer whenever the route changes.
  useEffect(() => {
    setDrawerOpen(false);
    setUserMenuOpen(false);
  }, [location.pathname]);

  // Close on Escape and outside click.
  useEffect(() => {
    if (!drawerOpen && !userMenuOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDrawerOpen(false);
        setUserMenuOpen(false);
      }
    };
    const onClick = (e: MouseEvent) => {
      const target = e.target as Node;
      if (drawerOpen && drawerRef.current && !drawerRef.current.contains(target)) {
        setDrawerOpen(false);
      }
      if (userMenuOpen && userMenuRef.current && !userMenuRef.current.contains(target)) {
        setUserMenuOpen(false);
      }
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClick);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClick);
    };
  }, [drawerOpen, userMenuOpen]);

  const items: NavItem[] = [
    { to: '/', label: t('nav.trading') },
    { to: '/markets', label: t('nav.markets') },
    { to: '/futures', label: t('nav.futures') },
    { to: '/amm', label: t('nav.amm') },
    { to: '/wallet', label: t('nav.wallet') },
    { to: '/account', label: t('nav.account'), authOnly: true },
    { to: '/settings', label: t('nav.settings'), authOnly: true },
    { to: '/admin', label: t('nav.admin'), adminOnly: true },
    { to: '/legal', label: t('nav.legal') },
  ];

  const isActive = (to: string) => {
    if (to === '/') return location.pathname === '/';
    return location.pathname === to || location.pathname.startsWith(to + '/');
  };

  const navLinks = (
    <>
      {items
        .filter((i) => (!i.adminOnly || isAdmin) && (!i.authOnly || !!user))
        .map((i) => (
          <Link
            key={i.to}
            to={i.to}
            className={cls(
              'relative px-2 py-1.5 text-sm transition-colors',
              isActive(i.to)
                ? 'font-semibold text-pri'
                : 'text-sec hover:text-pri'
            )}
          >
            {i.label}
            {isActive(i.to) && (
              <span className="absolute -bottom-1 left-1/2 h-0.5 w-6 -translate-x-1/2 rounded-full bg-cta" />
            )}
          </Link>
        ))}
    </>
  );

  const langSwitcher = (
    <div className="inline-flex items-center rounded border border-line bg-bg2 p-0.5 text-xs">
      {LANGS.map((l) => (
        <button
          key={l.code}
          onClick={() => setLanguage(l.code)}
          className={cls(
            'rounded px-2 py-1 font-medium transition-all',
            i18n.language === l.code
              ? 'bg-cta/15 text-cta'
              : 'text-sec hover:text-pri'
          )}
          aria-pressed={i18n.language === l.code}
        >
          {l.label}
        </button>
      ))}
    </div>
  );

  return (
    <header className="sticky top-0 z-40 flex h-16 items-center justify-between border-b border-line bg-bg1/95 px-4 backdrop-blur-md">
      <div className="flex items-center gap-6">
        <Link to="/" className="flex items-center gap-2">
          <span className="flex h-7 w-7 items-center justify-center rounded bg-cta text-sm font-bold text-bg1">
            F
          </span>
          <span className="text-lg font-semibold tracking-wide text-pri">
            Fliance<span className="ml-1 text-xs font-medium text-third">梵响</span>
          </span>
        </Link>
        <nav className="hidden items-center gap-1 md:flex">{navLinks}</nav>
      </div>
      <div className="flex items-center gap-2 sm:gap-3">
        <div className="hidden sm:block">{langSwitcher}</div>
        {user ? (
          <div ref={userMenuRef} className="relative">
            <button
              onClick={() => setUserMenuOpen((v) => !v)}
              className="flex items-center gap-2 rounded border border-line bg-bg2 px-2.5 py-1.5 text-sm text-pri transition-colors hover:bg-container"
            >
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-cta/20 text-xs font-semibold text-cta">
                {user.email.slice(0, 1).toUpperCase()}
              </span>
              <span className="hidden max-w-[120px] truncate sm:inline">{user.email}</span>
              <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4 text-third">
                <path d="M6 9l6 6 6-6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
            {userMenuOpen && (
              <div className="animate-fade-in absolute right-0 top-12 w-56 origin-top-right rounded-lg border border-line bg-bg1 p-1">
                <div className="border-b border-line px-3 py-2">
                  <div className="truncate text-sm text-pri">{user.email}</div>
                  <div className="mt-0.5 text-xs text-third">
                    {isAdmin ? t('nav.admin') : t('nav.account')} · {user.role}
                  </div>
                </div>
                <Link to="/account" className="block rounded px-3 py-2 text-sm text-pri hover:bg-container">
                  {t('nav.account')}
                </Link>
                <Link to="/settings" className="block rounded px-3 py-2 text-sm text-pri hover:bg-container">
                  {t('nav.settings')}
                </Link>
                <Link to="/wallet" className="block rounded px-3 py-2 text-sm text-pri hover:bg-container">
                  {t('nav.wallet')}
                </Link>
                <button
                  onClick={() => { logout(); navigate('/login'); }}
                  className="block w-full rounded px-3 py-2 text-left text-sm text-loss hover:bg-loss-bg"
                >
                  {t('auth.logout')}
                </button>
              </div>
            )}
          </div>
        ) : (
          <>
            <button
              onClick={() => navigate('/login')}
              className="px-2 py-1.5 text-sm font-medium text-pri transition-colors hover:text-cta"
            >
              {t('auth.signIn')}
            </button>
            <button
              onClick={() => navigate('/register')}
              className="hidden rounded bg-cta px-4 py-1.5 text-sm font-semibold text-bg1 transition-colors hover:bg-brand sm:inline-flex"
            >
              {t('auth.register')}
            </button>
          </>
        )}
        {/* Mobile hamburger */}
        <button
          className="inline-flex h-9 w-9 items-center justify-center rounded text-pri hover:bg-container md:hidden"
          onClick={() => setDrawerOpen((v) => !v)}
          aria-label="Menu"
          aria-expanded={drawerOpen}
        >
          {drawerOpen ? (
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M6 6l12 12M6 18L18 6" /></svg>
          ) : (
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
          )}
        </button>
      </div>

      {/* Mobile drawer */}
      {drawerOpen && (
        <div
          ref={drawerRef}
          className="animate-fade-in absolute right-0 top-16 z-50 w-64 origin-top-right rounded-bl-lg border-b border-l border-line bg-bg1 p-4 md:hidden"
        >
          <nav className="flex flex-col gap-1 text-sm">{navLinks}</nav>
          <div className="mt-4 border-t border-line pt-4">
            <div className="mb-2 text-xs text-third">{t('common.language')}</div>
            {langSwitcher}
          </div>
          {user && (
            <div className="mt-4 truncate border-t border-line pt-3 text-sm text-sec">
              {user.email}
            </div>
          )}
        </div>
      )}
    </header>
  );
}
