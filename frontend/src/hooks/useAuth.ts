import { useEffect, useState } from 'react';
import { useAuthStore } from '@/store/authStore';
import { getAccount } from '@/api/auth';

export function useAuthInit() {
  const { token, setAuth, logout } = useAuthStore();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!token) {
      setLoading(false);
      return;
    }
    getAccount()
      .then((res) => setAuth(token, res.user))
      .catch(() => logout())
      .finally(() => setLoading(false));
  }, [token, setAuth, logout]);

  return { loading, isAuthenticated: !!useAuthStore.getState().user };
}
