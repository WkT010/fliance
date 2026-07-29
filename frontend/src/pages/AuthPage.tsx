import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { login, register, getAccount } from '@/api/auth';
import { useAuthStore } from '@/store/authStore';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Card } from '@/components/common/Card';

export function LoginPage() {
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
      useAuthStore.getState().setAuth(res.access_token, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, account.user);
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number; data?: { error?: string } } };
        const status = axiosErr.response?.status;
        if (status === 401) { setError('Email or password incorrect'); return; }
        if (status === 429) { setError('Too many attempts. Please wait and try again'); return; }
        if (axiosErr.response?.data?.error) { setError(axiosErr.response.data.error); return; }
      }
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally { setLoading(false); }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-nexa-950 p-4">
      <Card className="w-full max-w-md p-6">
        <h1 className="mb-6 text-center text-2xl font-bold text-accent">NEXA</h1>
        <form onSubmit={submit} className="space-y-4">
          <Input label="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input label="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          {error && <div className="rounded border border-down/20 bg-down/10 px-3 py-2 text-sm text-down">{error}</div>}
          <Button type="submit" isLoading={loading} className="w-full">Sign In</Button>
        </form>
        <p className="mt-4 text-center text-sm text-nexa-400">No account? <Link to="/register" className="text-accent hover:underline">Register</Link></p>
      </Card>
    </div>
  );
}

export function RegisterPage() {
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
      useAuthStore.getState().setAuth(res.access_token, { id: '', email, role: 'user' });
      const account = await getAccount();
      setAuth(res.access_token, account.user);
      navigate('/');
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosErr = err as { response?: { status?: number } };
        if (axiosErr.response?.status === 409) { setError('already_registered'); return; }
      }
      setError(err instanceof Error ? err.message : 'Registration failed');
    } finally { setLoading(false); }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-nexa-950 p-4">
      <Card className="w-full max-w-md p-6">
        <h1 className="mb-6 text-center text-2xl font-bold text-accent">NEXA</h1>
        <form onSubmit={submit} className="space-y-4">
          <Input label="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input label="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          {error && error === 'already_registered' ? (
            <div className="rounded border border-down/20 bg-down/10 px-3 py-2 text-sm text-down">This email is already registered. <Link to="/login" className="text-accent font-medium hover:underline">Sign In</Link></div>
          ) : (error && <div className="rounded border border-down/20 bg-down/10 px-3 py-2 text-sm text-down">{error}</div>)}
          <Button type="submit" isLoading={loading} className="w-full">Create Account</Button>
        </form>
        <p className="mt-4 text-center text-sm text-nexa-400">Already have an account? <Link to="/login" className="text-accent hover:underline">Sign In</Link></p>
      </Card>
    </div>
  );
}