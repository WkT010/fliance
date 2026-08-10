import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { login, register, getAccount } from '@/api/auth';
import { useAuthStore } from '@/store/authStore';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { toast } from '@/store/toastStore';

function AuthShell({ children, title, subtitle }: { children: React.ReactNode; title: string; subtitle: string }) {
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-nexa-950 p-4">
      <div className="pointer-events-none absolute inset-0">
        <div className="absolute -top-40 left-1/4 h-96 w-96 rounded-full bg-accent/15 blur-[120px]" />
        <div className="absolute bottom-0 right-0 h-80 w-80 rounded-full bg-up/10 blur-[100px]" />
        <div
          className="absolute inset-0 opacity-[0.04]"
          style={{
            backgroundImage:
              'linear-gradient(to right, #fff 1px, transparent 1px), linear-gradient(to bottom, #fff 1px, transparent 1px)',
            backgroundSize: '40px 40px',
          }}
        />
      </div>
      <div className="relative w-full max-w-md animate-fade-in">
        <div className="mb-8 flex items-center justify-center gap-2">
          <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent text-lg font-bold text-nexa-950 shadow-lg shadow-accent/30">
            F
          </span>
          <span className="text-2xl font-bold text-nexa-100">
            Fliance<span className="ml-1.5 text-xs font-medium text-nexa-500">梵响</span>
          </span>
        </div>
        <div className="rounded-2xl border border-nexa-700/70 bg-nexa-800/60 p-7 shadow-2xl shadow-black/40 backdrop-blur-sm">
          <div className="mb-6 text-center">
            <h1 className="text-xl font-bold text-nexa-100">{title}</h1>
            <p className="mt-1 text-sm text-nexa-400">{subtitle}</p>
          </div>
          {children}
        </div>
      </div>
    </div>
  );
}

export function LoginPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const { setAuth } = useAuthStore();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (loading) return;
    setLoading(true);
    try {
      const res = await login({ email, password });
      useAuthStore.getState().setAuth(res.access_token, res.refresh_token ?? null, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, res.refresh_token ?? null, { id: account.user_id, email: account.email, role: account.role });
      toast.success(t('auth.welcomeBack'), account.email);
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number; data?: { error?: string } } };
        const msg = axiosErr.response?.data?.error || t('auth.invalidCredentials');
        toast.error(msg, t('auth.signInTitle'));
        return;
      }
      toast.error(err instanceof Error ? err.message : t('auth.invalidCredentials'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell title={t('auth.signInTitle')} subtitle={t('auth.welcomeBack')}>
      <form onSubmit={submit} className="space-y-4">
        <Input
          label={t('auth.email')}
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          autoComplete="email"
          icon={
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
              <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" strokeWidth="2" />
              <path d="M3 7l9 6 9-6" stroke="currentColor" strokeWidth="2" />
            </svg>
          }
        />
        <Input
          label={t('auth.password')}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          autoComplete="current-password"
          icon={
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
              <rect x="3" y="11" width="18" height="11" rx="2" stroke="currentColor" strokeWidth="2" />
              <path d="M7 11V7a5 5 0 0110 0v4" stroke="currentColor" strokeWidth="2" />
            </svg>
          }
        />
        <Button type="submit" isLoading={loading} block size="lg" className="mt-2">
          {t('auth.signInCta')}
        </Button>
      </form>
      <p className="mt-6 text-center text-sm text-nexa-400">
        {t('auth.noAccount')}{' '}
        <Link to="/register" className="font-semibold text-accent transition-colors hover:text-accent/80">
          {t('auth.register')}
        </Link>
      </p>
    </AuthShell>
  );
}

export function RegisterPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const { setAuth } = useAuthStore();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (loading) return;
    if (password.length < 8) {
      toast.warning(t('settings.passwordTooShort'));
      return;
    }
    setLoading(true);
    try {
      const res = await register({ email, password });
      useAuthStore.getState().setAuth(res.access_token, res.refresh_token ?? null, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, res.refresh_token ?? null, { id: account.user_id, email: account.email, role: account.role });
      toast.success(t('auth.welcomeBack'), account.email);
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number; data?: { error?: string } } };
        const status = axiosErr.response?.status;
        if (status === 409) {
          toast.error(t('auth.emailExists'));
          return;
        }
        toast.error(axiosErr.response?.data?.error || t('auth.emailExists'));
        return;
      }
      toast.error(err instanceof Error ? err.message : t('auth.emailExists'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell title={t('auth.registerTitle')} subtitle={t('auth.welcomeBack')}>
      <form onSubmit={submit} className="space-y-4">
        <Input
          label={t('auth.email')}
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          autoComplete="email"
          icon={
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
              <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" strokeWidth="2" />
              <path d="M3 7l9 6 9-6" stroke="currentColor" strokeWidth="2" />
            </svg>
          }
        />
        <Input
          label={t('auth.password')}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          autoComplete="new-password"
          hint="Minimum 8 characters"
          icon={
            <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
              <rect x="3" y="11" width="18" height="11" rx="2" stroke="currentColor" strokeWidth="2" />
              <path d="M7 11V7a5 5 0 0110 0v4" stroke="currentColor" strokeWidth="2" />
            </svg>
          }
        />
        <Button type="submit" isLoading={loading} block size="lg" className="mt-2">
          {t('auth.registerCta')}
        </Button>
      </form>
      <p className="mt-4 text-center text-xs leading-relaxed text-nexa-500">
        {t('auth.agreePrefix')}{' '}
        <Link to="/legal/terms" className="font-medium text-accent transition-colors hover:text-accent/80 hover:underline">
          {t('auth.agreeTerms')}
        </Link>
        {t('auth.agreeAnd')}
        <Link to="/legal/privacy" className="font-medium text-accent transition-colors hover:text-accent/80 hover:underline">
          {t('auth.agreePrivacy')}
        </Link>
      </p>
      <p className="mt-4 text-center text-sm text-nexa-400">
        {t('auth.hasAccount')}{' '}
        <Link to="/login" className="font-semibold text-accent transition-colors hover:text-accent/80">
          {t('auth.signIn')}
        </Link>
      </p>
    </AuthShell>
  );
}
