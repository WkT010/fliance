import { api } from './client';
import type { APIKey } from '@/types';

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
