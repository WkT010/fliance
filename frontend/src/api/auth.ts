import { api } from './client';
import type { LoginReq, RegisterReq, User } from '@/types';

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

export async function getAccount(): Promise<{ user: User }> {
  const res = await api.get<{ user: User }>('/account');
  return res.data;
}
