import { Routes, Route } from 'react-router-dom';
import { TradingPage } from '@/pages/TradingPage';
import { MarketsPage } from '@/pages/MarketsPage';
import { FuturesPage } from '@/pages/FuturesPage';
import { AMMPage } from '@/pages/AMMPage';
import { WalletPage } from '@/pages/WalletPage';
import { AccountPage } from '@/pages/AccountPage';
import { SettingsPage } from '@/pages/SettingsPage';
import { AdminPage } from '@/pages/AdminPage';
import { LegalPage } from '@/pages/LegalPage';
import { LandingPage } from '@/pages/LandingPage';
import { LoginPage, RegisterPage } from '@/pages/AuthPage';
import { ProtectedRoute } from '@/components/ProtectedRoute';
import { useAuthInit } from '@/hooks/useAuth';
import { useAuthStore } from '@/store/authStore';

export default function App() {
  const { loading } = useAuthInit();
  const { user } = useAuthStore();
  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-nexa-950 text-nexa-100">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-accent border-t-transparent" />
      </div>
    );
  }

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route path="/" element={user ? <ProtectedRoute><TradingPage /></ProtectedRoute> : <LandingPage />} />
      <Route path="/markets" element={<ProtectedRoute><MarketsPage /></ProtectedRoute>} />
      <Route path="/futures" element={<ProtectedRoute><FuturesPage /></ProtectedRoute>} />
      <Route path="/amm" element={<ProtectedRoute><AMMPage /></ProtectedRoute>} />
      <Route path="/wallet" element={<ProtectedRoute><WalletPage /></ProtectedRoute>} />
      <Route path="/account" element={<ProtectedRoute><AccountPage /></ProtectedRoute>} />
      <Route path="/settings" element={<ProtectedRoute><SettingsPage /></ProtectedRoute>} />
      <Route path="/admin" element={<ProtectedRoute admin><AdminPage /></ProtectedRoute>} />
      <Route path="/legal" element={<ProtectedRoute><LegalPage /></ProtectedRoute>} />
    </Routes>
  );
}
