import { api } from './client';
import type { WithdrawalReviewItem, AddressBookEntry, PairRiskConfig, KycSubmission, DepositClaimAdmin, PriceAdjustConfig } from '@/types';

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

// ── KYC review ──

/** GET /admin/kyc — list submissions, optionally filtered by status. */
export async function listKyc(status?: string): Promise<{ submissions: KycSubmission[]; limit: number; offset: number }> {
  const params = status ? `?status=${encodeURIComponent(status)}&limit=100&offset=0` : '?limit=100&offset=0';
  const res = await api.get(`/admin/kyc${params}`);
  return res.data;
}

/** POST /admin/kyc/:id/review — approve or reject a pending submission. */
export async function reviewKyc(id: string, action: 'approve' | 'reject', reason?: string): Promise<KycSubmission> {
  const res = await api.post(`/admin/kyc/${id}/review`, { action, reason: reason || '' });
  return res.data;
}

/**
 * GET /admin/kyc/:id/documents?type=front|back — fetch the stored identity
 * document image bytes and expose them as an object URL (the admin list only
 * carries a filesystem path, which a browser cannot render).
 */
export async function fetchKycDoc(id: string, type: 'front' | 'back'): Promise<string> {
  const res = await api.get(`/admin/kyc/${id}/documents`, {
    params: { type },
    responseType: 'blob',
  });
  return URL.createObjectURL(res.data as Blob);
}

// ── Deposit claim review ──

/** GET /admin/deposit/claims — list deposit claims, optionally filtered by status. */
export async function getDepositClaimsAdmin(status?: string, limit = 100, offset = 0): Promise<{ claims: DepositClaimAdmin[] }> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (status) params.set('status', status);
  const res = await api.get(`/admin/deposit/claims?${params.toString()}`);
  return res.data;
}

/** POST /admin/deposit/claims/:id/review — approve or reject a deposit claim. */
export async function reviewDepositClaim(id: string, action: 'approve' | 'reject', reason?: string): Promise<{ ok: boolean }> {
  const res = await api.post(`/admin/deposit/claims/${id}/review`, { action, reason: reason || '' });
  return res.data;
}

/**
 * GET /admin/deposit/claims/:id/screenshot — fetch the stored payment
 * screenshot bytes as an object URL. 404 when the user submitted none.
 */
export async function fetchDepositScreenshot(id: string): Promise<string> {
  const res = await api.get(`/admin/deposit/claims/${id}/screenshot`, {
    responseType: 'blob',
  });
  return URL.createObjectURL(res.data as Blob);
}

// ── Operator price adjustments ──

/** GET /admin/price-adjust/:pair — current multiplier/offset for a pair. */
export async function getPriceAdjust(pair: string): Promise<PriceAdjustConfig> {
  const res = await api.get(`/admin/price-adjust/${encodeURIComponent(pair)}`);
  return res.data;
}

/** PUT /admin/price-adjust/:pair — set multiplier (0.1~10) and offset. */
export async function setPriceAdjust(pair: string, multiplier: string, offset: string): Promise<PriceAdjustConfig> {
  const res = await api.put(`/admin/price-adjust/${encodeURIComponent(pair)}`, { multiplier, offset });
  return res.data;
}
