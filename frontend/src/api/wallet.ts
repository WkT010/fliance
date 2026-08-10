import { api } from './client';
import type { Balance, Transaction, WithdrawReq } from '@/types';

export async function getBalances(): Promise<Balance[]> {
  const res = await api.get<{ balances: Balance[] }>('/wallet/balances');
  return res.data.balances || [];
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
