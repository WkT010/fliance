import axios from 'axios';
import type { AxiosError, InternalAxiosRequestConfig } from 'axios';
import { API_BASE } from '@/utils/constants';
import { useAuthStore } from '@/store/authStore';

export const api = axios.create({
  baseURL: API_BASE,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

interface RetryableConfig extends InternalAxiosRequestConfig {
  /** Marks a request already retried once after a token refresh. */
  __retried?: boolean;
}

// Single-flight refresh: concurrent 401 responses share ONE refresh request
// instead of racing N parallel refreshes (the backend rotates refresh tokens,
// so only the first request to present a refresh token would succeed).
let refreshPromise: Promise<string> | null = null;

async function refreshAccessToken(): Promise<string> {
  const refreshToken = useAuthStore.getState().refreshToken;
  if (!refreshToken) throw new Error('no refresh token');
  // IMPORTANT: use a bare axios instance (not `api`) so the refresh request
  // itself never re-enters this 401 interceptor — that guarantees a failed
  // refresh can't loop into another refresh attempt.
  const res = await axios.post(`${API_BASE}/auth/refresh`, { refresh_token: refreshToken });
  const data = res.data as { access_token?: string; token?: string; refresh_token?: string };
  const access = data.access_token || data.token;
  if (!access) throw new Error('refresh returned no access token');
  // Refresh tokens rotate and are single-use: always persist the new pair.
  useAuthStore.getState().setTokens(access, data.refresh_token ?? null);
  return access;
}

function forceLogout() {
  useAuthStore.getState().logout();
  window.location.href = '/login';
}

/**
 * Extract a user-facing message from any API error. Prefers the backend's
 * error body ({"error": "..."} per internal/api/errors.go) over axios's
 * generic "Request failed with status code 400" text, so users see the real
 * reason (bad address, daily limit exceeded, …). Use in every user-visible
 * catch instead of `err.message` alone.
 */
export function getApiErrorMessage(err: unknown, fallback: string): string {
  const data = (err as { response?: { data?: unknown } } | null)?.response?.data;
  if (data && typeof data === 'object') {
    const body = data as { error?: unknown; message?: unknown };
    if (typeof body.error === 'string' && body.error.trim()) return body.error;
    if (typeof body.message === 'string' && body.message.trim()) return body.message;
  }
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}

api.interceptors.response.use(
  (res) => res,
  async (err: AxiosError) => {
    const config = err.config as RetryableConfig | undefined;
    if (err.response?.status !== 401 || !config) return Promise.reject(err);

    // Credential endpoints are final: a 401 on login/register/refresh means
    // bad credentials or a dead refresh token — no retry, no logout
    // (there is nothing to invalidate on the login page).
    const url = config.url || '';
    if (url.includes('/auth/login') || url.includes('/auth/register') || url.includes('/auth/refresh')) {
      return Promise.reject(err);
    }

    // Already retried once, or no refresh token available → session over.
    if (config.__retried || !useAuthStore.getState().refreshToken) {
      forceLogout();
      return Promise.reject(err);
    }
    config.__retried = true;
    try {
      refreshPromise ??= refreshAccessToken().finally(() => { refreshPromise = null; });
      const token = await refreshPromise;
      config.headers.Authorization = `Bearer ${token}`;
      return api(config);
    } catch {
      // Refresh failed (expired/revoked) — end the session.
      forceLogout();
      return Promise.reject(err);
    }
  }
);
