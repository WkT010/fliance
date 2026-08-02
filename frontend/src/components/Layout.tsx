import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Header } from './Header';

export function Layout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="flex h-screen flex-col overflow-hidden bg-nexa-950 text-nexa-100">
      <Header />
      <main className="flex-1 overflow-hidden">{children}</main>
      <footer className="flex h-10 flex-shrink-0 items-center justify-between border-t border-nexa-700 bg-nexa-900 px-4 text-xs text-nexa-400">
        <span>{t('footer.rights')}</span>
        <div className="hidden items-center gap-4 sm:flex">
          <Link to="/legal" className="hover:text-nexa-100">{t('footer.legal')}</Link>
          <Link to="/legal" className="hover:text-nexa-100">{t('footer.terms')}</Link>
          <Link to="/legal" className="hover:text-nexa-100">{t('footer.privacy')}</Link>
          <Link to="/legal" className="hover:text-nexa-100">{t('footer.risk')}</Link>
        </div>
        <span className="hidden lg:inline">{t('footer.trademark')}</span>
      </footer>
    </div>
  );
}
