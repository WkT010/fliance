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
      .then((res) => setAuth(token, useAuthStore.getState().refreshToken, { id: res.user_id, email: res.email, role: res.role, created_at: res.created_at }))
      .catch(() => logout())
      .finally(() => setLoading(false));
  }, [token, setAuth, logout]);

  return { loading, isAuthenticated: !!useAuthStore.getState().user };
}
