import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type { AuthState, User } from '@/types';

interface AuthStore extends AuthState {
  setAuth: (token: string, refreshToken: string | null, user: User) => void;
  setTokens: (token: string, refreshToken: string | null) => void;
  logout: () => void;
}

// SECURITY hardening for JWT storage:
// 1. Persisted to sessionStorage (not localStorage) so credentials die with
//    the browser session and are not shared across tabs opened later.
// 2. Tokens are never printed/logged anywhere in the codebase.
// RECOMMENDATION (requires backend change, out of scope here): move the
// access/refresh tokens into httpOnly, Secure, SameSite=Strict cookies so
// JavaScript can no longer read them at all, which fully neutralises XSS
// token theft. Until then, sessionStorage + no logging is the mitigation.
export const useAuthStore = create<AuthStore>()(
  persist(
    (set) => ({
      token: null,
      refreshToken: null,
      user: null,
      isAdmin: false,
      setAuth: (token, refreshToken, user) =>
        set({ token, refreshToken, user, isAdmin: user.role === 'admin' }),
      setTokens: (token, refreshToken) => set({ token, refreshToken }),
      logout: () => set({ token: null, refreshToken: null, user: null, isAdmin: false }),
    }),
    // NOTE (Fliance rebrand): storage key renamed from 'nexa-auth' to
    // 'fliance-auth'. Renaming intentionally invalidates existing users'
    // persisted sessions (they will be asked to sign in again) — accepted.
    { name: 'fliance-auth', storage: createJSONStorage(() => sessionStorage) }
  )
);
