import { api } from './client';
import type { LoginReq, RegisterReq } from '@/types';

export interface TokenResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
}

export async function login(data: LoginReq): Promise<TokenResponse> {
  const res = await api.post<TokenResponse>('/auth/login', data);
  return res.data;
}

export async function register(data: RegisterReq): Promise<TokenResponse> {
  const res = await api.post<TokenResponse>('/auth/register', data);
  return res.data;
}

export interface AccountResponse {
  user_id: string;
  email: string;
  role: 'user' | 'admin';
  created_at: number;
  balances?: Array<{ asset: string; available: string; locked: string }>;
}

export async function getAccount(): Promise<AccountResponse> {
  const res = await api.get<AccountResponse>('/account');
  return res.data;
}

export interface ChangePasswordReq {
  current_password: string;
  new_password: string;
}

export async function changePassword(data: ChangePasswordReq): Promise<{ status: string }> {
  const res = await api.post<{ status: string }>('/auth/change-password', data);
  return res.data;
}
