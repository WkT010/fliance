import { api } from './client';
import type { AccountType, Balance, DepositClaim, DepositClaimReq, Transaction, WithdrawReq } from '@/types';

/** Balance row as the wire format may omit account_type on older builds. */
type RawBalance = Omit<Balance, 'account_type'> & { account_type?: AccountType };

export async function getBalances(): Promise<Balance[]> {
  const res = await api.get<RawBalance[] | { balances: RawBalance[] }>('/wallet/balances');
  // The v2 endpoint returns a bare array; older builds wrapped it in { balances }.
  const rows = Array.isArray(res.data) ? res.data : res.data?.balances || [];
  // Same asset appears once per sub-account; default missing account_type to spot.
  return rows.map((b) => ({ ...b, account_type: b.account_type ?? 'spot' }));
}

export async function getTransactions(): Promise<Transaction[]> {
  const res = await api.get<{ transactions: Transaction[] }>('/wallet/transactions');
  return res.data.transactions || [];
}

export async function withdraw(data: WithdrawReq): Promise<{ tx_id: string; status: string }> {
  const res = await api.post('/wallet/withdraw', data);
  return res.data;
}

export async function getDepositAddress(asset: string): Promise<{ address: string }> {
  const res = await api.post('/wallet/deposit/address', { asset });
  return res.data;
}

// NOTE: the former POST /wallet/deposit self-credit endpoint was moved to
// the admin group (POST /admin/wallet/deposit) — letting any authenticated
// user mint balances was a privilege-escalation hole. Regular users now
// receive deposits via on-chain detection by the wallet-service; admins use
// adminDeposit() from '@/api/admin'.

export async function getSupportedAssets(): Promise<string[]> {
  const res = await api.get<{ assets: string[] }>('/wallet/assets');
  return res.data.assets || [];
}

/**
 * POST /wallet/deposit/claim — submit an on-chain deposit proof (txid
 * required, screenshot optional). 201 → { id, status, auto_verified }:
 * status is 'pending' for manual review, or 'approved' (with
 * auto_verified=true) when the on-chain Alchemy verification passed
 * immediately. 409 → duplicate txid, 400 → { error }.
 */
export async function submitDepositClaim(req: DepositClaimReq): Promise<{ id: string; status: string; auto_verified?: boolean }> {
  const res = await api.post('/wallet/deposit/claim', req);
  return res.data;
}

/** GET /wallet/deposit/claims — the caller's own deposit claim history. */
export async function getDepositClaims(): Promise<DepositClaim[]> {
  const res = await api.get<{ claims: DepositClaim[] }>('/wallet/deposit/claims');
  return res.data.claims || [];
}

/** Internal transfer between sub-accounts. 400 → { error } (e.g. "insufficient balance"). */
export async function transfer(
  from: AccountType,
  to: AccountType,
  asset: string,
  amount: string
): Promise<{ ok: boolean }> {
  const res = await api.post<{ ok: boolean }>('/wallet/transfer', { from, to, asset, amount });
  return res.data;
}
