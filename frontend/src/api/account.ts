import { api } from './client';
import type { APIKey, PnLSummary, PnLHistory } from '@/types';

export async function getPnL(): Promise<PnLSummary> {
  const res = await api.get<PnLSummary>('/account/pnl');
  return res.data;
}

export async function getPnLHistory(days = 30): Promise<PnLHistory> {
  const res = await api.get<PnLHistory>('/account/pnl/history', { params: { days } });
  return res.data;
}

export async function listAPIKeys(): Promise<APIKey[]> {
  const res = await api.get<{ api_keys: APIKey[] }>('/account/api-keys');
  return res.data.api_keys || [];
}

export async function createAPIKey(name: string): Promise<APIKey> {
  const res = await api.post<APIKey>('/account/api-keys', { name });
  return res.data;
}

export async function revokeAPIKey(id: string): Promise<void> {
  await api.delete(`/account/api-keys/${id}`);
}
