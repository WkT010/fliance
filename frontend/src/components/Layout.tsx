import { Link } from 'react-router-dom';
import { Header } from './Header';

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen flex-col bg-nexa-950 text-nexa-100">
      <Header />
      <main className="flex-1 overflow-hidden">{children}</main>
      <footer className="flex h-10 items-center justify-between border-t border-nexa-700 bg-nexa-900 px-4 text-xs text-nexa-400">
        <span>&copy; 2026 Canival Studios. All rights reserved.</span>
        <div className="flex items-center gap-4">
          <Link to="/legal" className="hover:text-nexa-100">Legal</Link>
          <Link to="/legal" className="hover:text-nexa-100">Terms</Link>
          <Link to="/legal" className="hover:text-nexa-100">Privacy</Link>
          <Link to="/legal" className="hover:text-nexa-100">Risk Disclosure</Link>
        </div>
        <span className="hidden sm:inline">Nexa Exchange&trade; and NEXA&trade; are trademarks of Canival Studios.</span>
      </footer>
    </div>
  );
}
