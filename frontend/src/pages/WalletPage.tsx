import { useState } from 'react';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { getBalances, getTransactions, withdraw, getDepositAddress } from '@/api/wallet';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatQty, formatDate } from '@/utils/format';

export function WalletPage() {
  const { data: balances, refetch: refetchBalances } = useFetch(getBalances, []);
  const { data: txs, refetch: refetchTxs } = useFetch(getTransactions, []);
  usePolling(() => { refetchBalances(); refetchTxs(); }, 5000);

  const [asset, setAsset] = useState('BTC');
  const [address, setAddress] = useState('');
  const [amount, setAmount] = useState('');
  const [withdrawing, setWithdrawing] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  const submitWithdraw = async (e: React.FormEvent) => {
    e.preventDefault();
    setWithdrawing(true);
    setMsg(null);
    try {
      await withdraw({ asset, address, amount });
      setMsg('Withdrawal submitted for review');
      setAddress('');
      setAmount('');
      refetchTxs();
    } catch (err: unknown) {
      setMsg(err instanceof Error ? err.message : 'Withdrawal failed');
    } finally {
      setWithdrawing(false);
    }
  };

  const deposit = async () => {
    try {
      const res = await getDepositAddress(asset);
      setMsg(`Deposit address for ${asset}: ${res.address}`);
    } catch (err: unknown) {
      setMsg(err instanceof Error ? err.message : 'Failed');
    }
  };

  const balanceRows = (balances || []).map((b) => (
    <tr key={b.asset} className="border-b border-nexa-700/50">
      <td className="px-4 py-2 font-medium">{b.asset}</td>
      <td className="px-4 py-2 font-mono">{formatQty(b.available, 8)}</td>
      <td className="px-4 py-2 font-mono text-nexa-400">{formatQty(b.locked, 8)}</td>
      <td className="px-4 py-2 font-mono">{formatQty(b.total, 8)}</td>
    </tr>
  ));

  return (
    <Layout>
      <div className="grid h-full grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        <Card className="lg:col-span-2" title="Balances">
          <table className="w-full text-left text-sm">
            <thead className="text-nexa-400"><tr><th className="px-4 py-2">Asset</th><th className="px-4 py-2">Available</th><th className="px-4 py-2">Locked</th><th className="px-4 py-2">Total</th></tr></thead>
            <tbody>{balanceRows}</tbody>
          </table>
        </Card>
        <Card title="Actions">
          <Tabs
            defaultTab="withdraw"
            tabs={[
              {
                id: 'withdraw',
                label: 'Withdraw',
                content: (
                  <form onSubmit={submitWithdraw} className="space-y-3">
                    <Input label="Asset" value={asset} onChange={(e) => setAsset(e.target.value)} />
                    <Input label="Address" value={address} onChange={(e) => setAddress(e.target.value)} />
                    <Input label="Amount" type="number" step="0.000001" value={amount} onChange={(e) => setAmount(e.target.value)} />
                    <Button type="submit" variant="danger" isLoading={withdrawing}>Withdraw</Button>
                  </form>
                ),
              },
              {
                id: 'deposit',
                label: 'Deposit',
                content: (
                  <div className="space-y-3">
                    <Input label="Asset" value={asset} onChange={(e) => setAsset(e.target.value)} />
                    <Button onClick={deposit}>Get Deposit Address</Button>
                  </div>
                ),
              },
            ]}
          />
          {msg && <div className="mt-3 break-words rounded bg-nexa-900 p-2 text-xs text-nexa-300">{msg}</div>}
        </Card>
        <Card className="lg:col-span-3" title="Transactions">
          <table className="w-full text-left text-sm">
            <thead className="text-nexa-400">
              <tr><th className="px-4 py-2">Type</th><th className="px-4 py-2">Asset</th><th className="px-4 py-2">Amount</th><th className="px-4 py-2">Status</th><th className="px-4 py-2">Date</th></tr>
            </thead>
            <tbody>
              {(txs || []).map((t) => (
                <tr key={t.id} className="border-b border-nexa-700/50">
                  <td className="px-4 py-2 capitalize">{t.type}</td>
                  <td className="px-4 py-2">{t.asset}</td>
                  <td className="px-4 py-2 font-mono">{formatPrice(t.amount, 8)}</td>
                  <td className="px-4 py-2"><Badge>{t.status}</Badge></td>
                  <td className="px-4 py-2 text-nexa-400">{formatDate(t.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </div>
    </Layout>
  );
}
