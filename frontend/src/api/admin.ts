import { api } from './client';
import type { WithdrawalReviewItem, AddressBookEntry, PairRiskConfig } from '@/types';

export async function listWithdrawals(status?: string): Promise<{ withdrawals: WithdrawalReviewItem[]; limit: number; offset: number }> {
  const params = status ? `?status=${encodeURIComponent(status)}` : '';
  const res = await api.get(`/admin/withdrawals${params}`);
  return res.data;
}

export async function approveWithdrawal(id: string): Promise<void> {
  await api.post(`/admin/withdrawals/${id}/approve`);
}

export async function rejectWithdrawal(id: string): Promise<void> {
  await api.post(`/admin/withdrawals/${id}/reject`);
}

export async function listUserWithdrawals(userId: string): Promise<{ withdrawals: WithdrawalReviewItem[] }> {
  const res = await api.get(`/admin/users/${userId}/withdrawals`);
  return res.data;
}

export async function listUserAddresses(userId: string, asset?: string): Promise<{ addresses: AddressBookEntry[] }> {
  const params = asset ? `?asset=${encodeURIComponent(asset)}` : '';
  const res = await api.get(`/admin/users/${userId}/addresses${params}`);
  return res.data;
}

export async function addUserAddress(userId: string, entry: Omit<AddressBookEntry, 'id' | 'created_at'>): Promise<void> {
  await api.post(`/admin/users/${userId}/addresses`, entry);
}

export async function listPairRisk(): Promise<{ pairs: PairRiskConfig[] }> {
  const res = await api.get('/admin/risk/pairs');
  return res.data;
}

export async function getPairRisk(pair: string): Promise<PairRiskConfig> {
  const res = await api.get(`/admin/risk/pairs/${encodeURIComponent(pair)}`);
  return res.data;
}

export async function updatePairRisk(pair: string, cfg: Partial<PairRiskConfig>): Promise<PairRiskConfig> {
  const res = await api.put(`/admin/risk/pairs/${encodeURIComponent(pair)}`, cfg);
  return res.data;
}

export async function pausePair(pair: string): Promise<void> {
  await api.post(`/admin/pairs/${encodeURIComponent(pair)}/pause`);
}

export async function resumePair(pair: string): Promise<void> {
  await api.post(`/admin/pairs/${encodeURIComponent(pair)}/resume`);
}

export async function setUserDailyLimit(userId: string, asset: string, dailyLimit: string): Promise<void> {
  await api.post(`/admin/users/${userId}/limits`, { asset, daily_limit: dailyLimit });
}

// Manual balance credit. Formerly POST /wallet/deposit in the user group
// (self-service credit = privilege escalation); the backend moved it to the
// admin group, so only admins can call POST /admin/wallet/deposit now.
export async function adminDeposit(asset: string, amount: string, txHash?: string): Promise<{ status: string; asset: string; amount: string }> {
  const res = await api.post('/admin/wallet/deposit', { asset, amount, tx_hash: txHash });
  return res.data;
}

// ── AMM admin ──

export interface AmmSimulatorStatus {
  running?: boolean;
  configured?: boolean;
  interval_ms?: number;
  prices?: Record<string, string>;
  pairs?: string[];
  status?: string;
}

// Re-seed default AMM pools with bootstrap liquidity.
export async function seedAmmPools(): Promise<{ status: string; pairs: string[] }> {
  const res = await api.post('/admin/amm/seed');
  return res.data;
}

export async function startAmmSimulator(): Promise<AmmSimulatorStatus> {
  const res = await api.post('/admin/amm/simulator/start');
  return res.data;
}

export async function stopAmmSimulator(): Promise<AmmSimulatorStatus> {
  const res = await api.post('/admin/amm/simulator/stop');
  return res.data;
}

export async function getAmmSimulatorStatus(): Promise<AmmSimulatorStatus> {
  const res = await api.get('/admin/amm/simulator');
  return res.data;
}
