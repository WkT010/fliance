import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { login, register, getAccount } from '@/api/auth';
import { useAuthStore } from '@/store/authStore';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Card } from '@/components/common/Card';

export function LoginPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const { setAuth } = useAuthStore();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await login({ email, password });
      // Set token FIRST so getAccount() can authenticate via axios interceptor
      useAuthStore.getState().setAuth(res.access_token, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, { id: account.user_id, email: account.email, role: account.role });
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number; data?: { error?: string } } };
        const status = axiosErr.response?.status;
        if (status === 401) { setError(t('auth.invalidCredentials')); return; }
        if (status === 400) { setError(axiosErr.response?.data?.error || t('auth.invalidCredentials')); return; }
        if (status === 429) { setError(t('auth.invalidCredentials')); return; }
        if (axiosErr.response?.data?.error) {
          setError(axiosErr.response.data.error);
          return;
        }
      }
      setError(err instanceof Error ? err.message : t('auth.invalidCredentials'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-nexa-950 p-4">
      <Card className="w-full max-w-md p-6">
        <h1 className="mb-6 text-center text-2xl font-bold text-accent">{t('brand')}</h1>
        <form onSubmit={submit} className="space-y-4">
          <Input label={t('auth.email')} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input label={t('auth.password')} type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          {error && (
            <div className="rounded border border-down/20 bg-down/10 px-3 py-2 text-sm text-down">{error}</div>
          )}
          <Button type="submit" isLoading={loading} className="w-full">{t('auth.signIn')}</Button>
        </form>
        <p className="mt-4 text-center text-sm text-nexa-400">
          {t('auth.noAccount')} <Link to="/register" className="text-accent hover:underline">{t('auth.register')}</Link>
        </p>
      </Card>
    </div>
  );
}

export function RegisterPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const { setAuth } = useAuthStore();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await register({ email, password });
      // Set token FIRST so getAccount() can authenticate
      useAuthStore.getState().setAuth(res.access_token, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, { id: account.user_id, email: account.email, role: account.role });
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number; data?: { error?: string } } };
        if (axiosErr.response?.status === 409) { setError('already_registered'); return; }
        if (axiosErr.response?.status === 400) { setError(axiosErr.response?.data?.error || t('auth.emailExists')); return; }
      }
      setError(err instanceof Error ? err.message : t('auth.emailExists'));
    } finally {
      setLoading(false);
    }

  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-nexa-950 p-4">
      <Card className="w-full max-w-md p-6">
        <h1 className="mb-6 text-center text-2xl font-bold text-accent">{t('brand')}</h1>
        <form onSubmit={submit} className="space-y-4">
          <Input label={t('auth.email')} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input label={t('auth.password')} type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          {error && error === 'already_registered' ? (
            <div className="rounded border border-down/20 bg-down/10 px-3 py-2 text-sm text-down">
              {t('auth.emailExists')}{' '}
              <Link to="/login" className="text-accent font-medium hover:underline">{t('auth.signIn')}</Link>
            </div>
          ) : (
            error && <p className="text-sm text-down">{error}</p>
          )}
          <Button type="submit" isLoading={loading} className="w-full">{t('auth.register')}</Button>
        </form>
        <p className="mt-4 text-center text-sm text-nexa-400">
          {t('auth.hasAccount')} <Link to="/login" className="text-accent hover:underline">{t('auth.signIn')}</Link>
        </p>
      </Card>
    </div>
  );
}





