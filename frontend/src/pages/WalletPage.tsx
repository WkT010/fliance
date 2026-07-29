import { useEffect, useMemo, useState } from 'react';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { Select } from '@/components/common/Select';
import { getBalances, getTransactions, withdraw, getDepositAddress, getSupportedAssets } from '@/api/wallet';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatQty, formatDate, cls } from '@/utils/format';
import type { Balance, Transaction } from '@/types';

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch { /* ignore */ }
  };
  return (
    <Button size="sm" variant="ghost" onClick={copy}>
      {copied ? 'Copied' : 'Copy'}
    </Button>
  );
}

export function WalletPage() {
  const { data: balances, refetch: refetchBalances } = useFetch(getBalances, []);
  const { data: txs, refetch: refetchTxs } = useFetch(getTransactions, []);
  const { data: supportedAssets } = useFetch(getSupportedAssets, []);
  usePolling(() => { refetchBalances(); refetchTxs(); }, 5000);

  const assets = useMemo(() => {
    const set = new Set<string>(supportedAssets || []);
    (balances || []).forEach((b) => set.add(b.asset));
    return Array.from(set).sort();
  }, [supportedAssets, balances]);

  const [asset, setAsset] = useState(assets[0] || 'BTC');
  useEffect(() => {
    if (assets.length && !assets.includes(asset)) setAsset(assets[0]);
  }, [assets, asset]);

  const [address, setAddress] = useState('');
  const [amount, setAmount] = useState('');
  const [withdrawing, setWithdrawing] = useState(false);
  const [msg, setMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

  const [depositAddress, setDepositAddress] = useState('');
  const [depositLoading, setDepositLoading] = useState(false);

  const [txFilter, setTxFilter] = useState<'all' | 'deposit' | 'withdrawal'>('all');
  const [txStatus, setTxStatus] = useState<'all' | 'pending' | 'completed' | 'failed'>('all');

  const selectedBalance = useMemo(() =>
    (balances || []).find((b) => b.asset === asset),
    [balances, asset]
  );

  const submitWithdraw = async (e: React.FormEvent) => {
    e.preventDefault();
    setMsg(null);
    setWithdrawing(true);
    try {
      await withdraw({ asset, address, amount });
      setMsg({ text: 'Withdrawal submitted for review', type: 'success' });
      setAddress('');
      setAmount('');
      refetchTxs();
      refetchBalances();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Withdrawal failed', type: 'error' });
    } finally {
      setWithdrawing(false);
    }
  };

  const deposit = async () => {
    setDepositLoading(true);
    setMsg(null);
    try {
      const res = await getDepositAddress(asset);
      setDepositAddress(res.address || '');
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      setDepositLoading(false);
    }
  };

  const filteredTxs = useMemo(() => {
    return (txs || []).filter((t) => {
      if (txFilter !== 'all' && t.type !== txFilter) return false;
      if (txStatus !== 'all' && t.status.toLowerCase() !== txStatus) return false;
      return true;
    });
  }, [txs, txFilter, txStatus]);

  const totalBalance = useMemo(() =>
    (balances || []).reduce((sum, b) => sum + parseFloat(b.total || '0'), 0),
    [balances]
  );

  return (
    <Layout>
      <div className="mx-auto grid max-w-6xl grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        {/* Overview */}
        <Card className="lg:col-span-2" title="Asset Overview">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400">
                <tr>
                  <th className="px-4 py-3">Asset</th>
                  <th className="px-4 py-3">Available</th>
                  <th className="px-4 py-3">Locked</th>
                  <th className="px-4 py-3 text-right">Total</th>
                </tr>
              </thead>
              <tbody>
                {(balances || []).length === 0 && (
                  <tr><td colSpan={4} className="px-4 py-6 text-center text-nexa-500">No balances</td></tr>
                )}
                {(balances || []).map((b) => (
                  <tr
                    key={b.asset}
                    className={cls(
                      'cursor-pointer border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30',
                      b.asset === asset && 'bg-nexa-800/40'
                    )}
                    onClick={() => setAsset(b.asset)}
                  >
                    <td className="px-4 py-3 font-medium">{b.asset}</td>
                    <td className="px-4 py-3 font-mono">{formatQty(b.available, 8)}</td>
                    <td className="px-4 py-3 font-mono text-nexa-400">{formatQty(b.locked, 8)}</td>
                    <td className="px-4 py-3 text-right font-mono">{formatQty(b.total, 8)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>

        <Card title="Total Estimated Value">
          <div className="flex h-full flex-col justify-center p-6">
            <div className="text-3xl font-bold text-nexa-100">{formatPrice(totalBalance.toString(), 6)}</div>
            <div className="mt-1 text-sm text-nexa-400">Total across all assets</div>
            <div className="mt-4 text-xs text-nexa-500">
              {balances?.length || 0} asset{(balances?.length || 0) === 1 ? '' : 's'}
            </div>
          </div>
        </Card>

        {/* Actions */}
        <Card className="lg:col-span-1" title="Actions">
          <div className="p-2">
            <Tabs
              defaultTab="deposit"
              tabs={[
                {
                  id: 'deposit',
                  label: 'Deposit',
                  content: (
                    <div className="space-y-4 pt-2">
                      <Select label="Asset" value={asset} onChange={(e) => { setAsset(e.target.value); setDepositAddress(''); }}>
                        {assets.map((a) => <option key={a} value={a}>{a}</option>)}
                      </Select>
                      {selectedBalance && (
                        <div className="text-xs text-nexa-400">
                          Available: <span className="font-mono text-nexa-200">{formatQty(selectedBalance.available, 8)} {asset}</span>
                        </div>
                      )}
                      <Button onClick={deposit} isLoading={depositLoading} className="w-full">
                        Get Deposit Address
                      </Button>
                      {depositAddress && (
                        <div className="space-y-2 rounded border border-nexa-700 bg-nexa-900/50 p-3">
                          <div className="text-xs text-nexa-500">Deposit Address</div>
                          <div className="break-all font-mono text-sm text-nexa-200">{depositAddress}</div>
                          <div className="flex gap-2">
                            <CopyButton text={depositAddress} />
                          </div>
                        </div>
                      )}
                      {depositAddress === '' && !depositLoading && (
                        <div className="text-xs text-nexa-500">
                          Generate an address to deposit {asset}. Deposits may require network confirmations.
                        </div>
                      )}
                    </div>
                  ),
                },
                {
                  id: 'withdraw',
                  label: 'Withdraw',
                  content: (
                    <form onSubmit={submitWithdraw} className="space-y-4 pt-2">
                      <Select label="Asset" value={asset} onChange={(e) => setAsset(e.target.value)}>
                        {assets.map((a) => <option key={a} value={a}>{a}</option>)}
                      </Select>
                      {selectedBalance && (
                        <div className="text-xs text-nexa-400">
                          Available: <span className="font-mono text-nexa-200">{formatQty(selectedBalance.available, 8)} {asset}</span>
                        </div>
                      )}
                      <Input label="Destination Address" value={address} onChange={(e) => setAddress(e.target.value)} required />
                      <Input label="Amount" type="number" step="0.000001" value={amount} onChange={(e) => setAmount(e.target.value)} required />
                      {msg && (
                        <div className={cls('rounded px-3 py-2 text-sm border', msg.type === 'success' ? 'bg-up/10 text-up border-up/20' : 'bg-down/10 text-down border-down/20')}>
                          {msg.text}
                        </div>
                      )}
                      <Button type="submit" variant="danger" isLoading={withdrawing} className="w-full">
                        Withdraw
                      </Button>
                    </form>
                  ),
                },
              ]}
            />
          </div>
        </Card>

        {/* Transactions */}
        <Card className="lg:col-span-2" title="Transaction History">
          <div className="space-y-3 p-4">
            <div className="flex flex-wrap gap-3">
              <Select label="" value={txFilter} onChange={(e) => setTxFilter(e.target.value as typeof txFilter)} className="w-36">
                <option value="all">All Types</option>
                <option value="deposit">Deposit</option>
                <option value="withdrawal">Withdrawal</option>
              </Select>
              <Select label="" value={txStatus} onChange={(e) => setTxStatus(e.target.value as typeof txStatus)} className="w-36">
                <option value="all">All Status</option>
                <option value="pending">Pending</option>
                <option value="completed">Completed</option>
                <option value="failed">Failed</option>
              </Select>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-nexa-400">
                  <tr>
                    <th className="px-3 py-2">Type</th>
                    <th className="px-3 py-2">Asset</th>
                    <th className="px-3 py-2">Amount</th>
                    <th className="px-3 py-2">Status</th>
                    <th className="px-3 py-2">Date</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTxs.length === 0 && (
                    <tr><td colSpan={5} className="px-3 py-6 text-center text-nexa-500">No matching transactions</td></tr>
                  )}
                  {filteredTxs.map((t) => (
                    <tr key={t.id} className="border-b border-nexa-800/50">
                      <td className="px-3 py-2 capitalize">{t.type}</td>
                      <td className="px-3 py-2">{t.asset}</td>
                      <td className="px-3 py-2 font-mono">{formatPrice(t.amount, 8)}</td>
                      <td className="px-3 py-2"><Badge>{t.status}</Badge></td>
                      <td className="px-3 py-2 text-nexa-400">{formatDate(t.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </Card>
      </div>
    </Layout>
  );
}
