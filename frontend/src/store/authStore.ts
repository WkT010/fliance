import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { AuthState, User } from '@/types';

interface AuthStore extends AuthState {
  setAuth: (token: string, user: User) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthStore>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      isAdmin: false,
      setAuth: (token, user) => set({ token, user, isAdmin: user.role === 'admin' }),
      logout: () => set({ token: null, user: null, isAdmin: false }),
    }),
    { name: 'nexa-auth' }
  )
);
