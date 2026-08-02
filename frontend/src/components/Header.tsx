import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/store/authStore';
import { Button } from './common/Button';
import { cls } from '@/utils/format';
import { LANGS, setLanguage } from '@/i18n';

export function Header() {
  const { t, i18n } = useTranslation();
  const { user, isAdmin, logout } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const drawerRef = useRef<HTMLDivElement>(null);

  // Close the mobile drawer whenever the route changes.
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

  // Close the mobile drawer on Escape and when clicking outside.
  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setDrawerOpen(false); };
    const onClick = (e: MouseEvent) => {
      if (drawerRef.current && !drawerRef.current.contains(e.target as Node)) setDrawerOpen(false);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClick);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClick);
    };
  }, [drawerOpen]);

  const navLinks = (
    <>
      <Link to="/" className="text-nexa-300 hover:text-nexa-100">{t('nav.trading')}</Link>
      <Link to="/markets" className="text-nexa-300 hover:text-nexa-100">{t('nav.markets')}</Link>
      <Link to="/futures" className="text-nexa-300 hover:text-nexa-100">{t('nav.futures')}</Link>
      <Link to="/amm" className="text-nexa-300 hover:text-nexa-100">{t('nav.amm')}</Link>
      <Link to="/wallet" className="text-nexa-300 hover:text-nexa-100">{t('nav.wallet')}</Link>
      <Link to="/account" className="text-nexa-300 hover:text-nexa-100">{t('nav.account')}</Link>
      <Link to="/settings" className="text-nexa-300 hover:text-nexa-100">{t('nav.settings')}</Link>
      {isAdmin && <Link to="/admin" className="text-nexa-300 hover:text-nexa-100">{t('nav.admin')}</Link>}
      <Link to="/legal" className="text-nexa-300 hover:text-nexa-100">{t('nav.legal')}</Link>
    </>
  );

  const langSwitcher = (
    <div className="flex items-center gap-1 text-xs">
      {LANGS.map((l) => (
        <button
          key={l.code}
          onClick={() => setLanguage(l.code)}
          className={cls(
            'rounded px-2 py-1 transition-colors',
            i18n.language === l.code
              ? 'bg-accent/20 text-accent'
              : 'text-nexa-400 hover:text-nexa-100'
          )}
          aria-pressed={i18n.language === l.code}
        >
          {l.label}
        </button>
      ))}
    </div>
  );

  return (
    <header className="relative flex h-14 items-center justify-between border-b border-nexa-700 bg-nexa-900 px-4">
      <div className="flex items-center gap-6">
        <Link to="/" className="text-xl font-bold text-accent">{t('brand')}</Link>
        <nav className="hidden gap-4 text-sm md:flex">{navLinks}</nav>
      </div>
      <div className="flex items-center gap-3">
        {langSwitcher}
        {user ? (
          <>
            <span className="hidden text-sm text-nexa-300 sm:inline">{user.email}</span>
            <Button size="sm" variant="ghost" onClick={() => { logout(); navigate('/login'); }}>{t('auth.logout')}</Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="ghost" onClick={() => navigate('/login')}>{t('auth.signIn')}</Button>
            <Button size="sm" onClick={() => navigate('/register')} className="hidden sm:inline-flex">{t('auth.register')}</Button>
          </>
        )}
        {/* Mobile hamburger */}
        <button
          className="inline-flex h-9 w-9 items-center justify-center rounded text-nexa-200 hover:bg-nexa-800 md:hidden"
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
          className="absolute right-0 top-14 z-50 w-64 origin-top-right border-b border-l border-nexa-700 bg-nexa-900 p-4 shadow-xl md:hidden"
        >
          <nav className="flex flex-col gap-3 text-sm">
            {navLinks}
          </nav>
          <div className="mt-4 border-t border-nexa-700 pt-4">
            <div className="mb-2 text-xs text-nexa-500">{t('common.language')}</div>
            {langSwitcher}
          </div>
          {user && (
            <div className="mt-4 truncate border-t border-nexa-700 pt-3 text-sm text-nexa-300">
              {user.email}
            </div>
          )}
        </div>
      )}
    </header>
  );
}
