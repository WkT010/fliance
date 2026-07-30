import { Link, useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/authStore';
import { Button } from './common/Button';

export function Header() {
  const { user, isAdmin, logout } = useAuthStore();
  const navigate = useNavigate();

  return (
    <header className="flex h-14 items-center justify-between border-b border-nexa-700 bg-nexa-900 px-4">
      <div className="flex items-center gap-6">
        <Link to="/" className="text-xl font-bold text-accent">NEXA</Link>
        <nav className="hidden gap-4 text-sm md:flex">
          <Link to="/" className="text-nexa-300 hover:text-nexa-100">Trading</Link>
          <Link to="/markets" className="text-nexa-300 hover:text-nexa-100">Markets</Link>
          <Link to="/futures" className="text-nexa-300 hover:text-nexa-100">Futures</Link>
          <Link to="/wallet" className="text-nexa-300 hover:text-nexa-100">Wallet</Link>
          <Link to="/account" className="text-nexa-300 hover:text-nexa-100">Account</Link>
          <Link to="/settings" className="text-nexa-300 hover:text-nexa-100">Settings</Link>
          {isAdmin && <Link to="/admin" className="text-nexa-300 hover:text-nexa-100">Admin</Link>}
          <Link to="/legal" className="text-nexa-300 hover:text-nexa-100">Legal</Link>
        </nav>
      </div>
      <div className="flex items-center gap-3">
        {user ? (
          <>
            <span className="text-sm text-nexa-300">{user.email}</span>
            <Button size="sm" variant="ghost" onClick={() => { logout(); navigate('/login'); }}>Logout</Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="ghost" onClick={() => navigate('/login')}>Sign In</Button>
            <Button size="sm" onClick={() => navigate('/register')}>Register</Button>
          </>
        )}
      </div>
    </header>
  );
}
