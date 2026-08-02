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

// deposit credits the authenticated user's wallet immediately. In production
// deposits are detected on-chain by the wallet-service; on this self-contained
// exchange (no real chain) it is a simulated credit that works for every
// asset, including internally-issued USDT which has no blockchain client.
export async function deposit(asset: string, amount: string, txHash?: string): Promise<{ status: string; asset: string; amount: string }> {
  const res = await api.post('/wallet/deposit', { asset, amount, tx_hash: txHash });
  return res.data;
}

export async function getSupportedAssets(): Promise<string[]> {
  const res = await api.get<{ assets: string[] }>('/wallet/assets');
  return res.data.assets || [];
}
