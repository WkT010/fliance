import { useState } from 'react';
import { Layout } from '@/components/Layout';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Badge } from '@/components/common/Badge';
import { Input } from '@/components/common/Input';
import {
  listWithdrawals, approveWithdrawal, rejectWithdrawal,
  listPairRisk, updatePairRisk, setUserDailyLimit,
} from '@/api/admin';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatDate } from '@/utils/format';
import type { WithdrawalReviewItem, PairRiskConfig } from '@/types';

export function AdminPage() {
  const { data: withdrawals, refetch: refetchW } = useFetch(() => listWithdrawals('pending'), []);
  const { data: riskPairs, refetch: refetchRisk } = useFetch(listPairRisk, []);
  usePolling(refetchW, 5000);

  const [userId, setUserId] = useState('');
  const [limitAsset, setLimitAsset] = useState('BTC');
  const [limitValue, setLimitValue] = useState('');

  const updateRisk = async (pair: string, patch: Partial<PairRiskConfig>) => {
    await updatePairRisk(pair, patch);
    refetchRisk();
  };

  return (
    <Layout>
      <div className="h-full p-4">
        <Tabs
          defaultTab="withdrawals"
          tabs={[
            {
              id: 'withdrawals',
              label: 'Withdrawal Review',
              content: (
                <div className="overflow-auto">
                  <table className="w-full text-left text-sm">
                    <thead className="text-nexa-400">
                      <tr><th className="py-2">ID</th><th className="py-2">User</th><th className="py-2">Asset</th><th className="py-2">Amount</th><th className="py-2">Status</th><th className="py-2">Time</th><th></th></tr>
                    </thead>
                    <tbody>
                      {(withdrawals?.withdrawals || []).map((w: WithdrawalReviewItem) => (
                        <tr key={w.id} className="border-b border-nexa-700/50">
                          <td className="py-2 font-mono text-xs">{w.id.slice(0, 12)}...</td>
                          <td className="py-2">{w.user_id}</td>
                          <td className="py-2">{w.asset}</td>
                          <td className="py-2 font-mono">{formatPrice(w.amount, 6)}</td>
                          <td className="py-2"><Badge>{w.status}</Badge></td>
                          <td className="py-2 text-nexa-400">{formatDate(w.created_at)}</td>
                          <td className="py-2">
                            <div className="flex gap-2">
                              <Button size="sm" variant="success" onClick={() => approveWithdrawal(w.id).then(refetchW)}>Approve</Button>
                              <Button size="sm" variant="danger" onClick={() => rejectWithdrawal(w.id).then(refetchW)}>Reject</Button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ),
            },
            {
              id: 'risk',
              label: 'Risk Controls',
              content: (
                <div className="space-y-3">
                  {(riskPairs?.pairs || []).map((cfg: PairRiskConfig) => (
                    <div key={cfg.pair} className="rounded border border-nexa-700 bg-nexa-900 p-3">
                      <div className="mb-2 flex items-center justify-between">
                        <span className="font-medium text-nexa-100">{cfg.pair}</span>
                        <Badge color={cfg.trading_enabled ? 'up' : 'down'}>{cfg.trading_enabled ? 'Active' : 'Paused'}</Badge>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button size="sm" variant="secondary" onClick={() => updateRisk(cfg.pair, { trading_enabled: false })}>Pause</Button>
                        <Button size="sm" variant="success" onClick={() => updateRisk(cfg.pair, { trading_enabled: true })}>Resume</Button>
                        <Button size="sm" variant="secondary" onClick={() => updateRisk(cfg.pair, { market_orders_enabled: !cfg.market_orders_enabled })}>
                          {cfg.market_orders_enabled ? 'Disable Market' : 'Enable Market'}
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              ),
            },
            {
              id: 'limits',
              label: 'User Limits',
              content: (
                <form
                  onSubmit={async (e) => {
                    e.preventDefault();
                    await setUserDailyLimit(userId, limitAsset, limitValue);
                    setLimitValue('');
                  }}
                  className="max-w-md space-y-3"
                >
                  <Input label="User ID" value={userId} onChange={(e) => setUserId(e.target.value)} required />
                  <Input label="Asset" value={limitAsset} onChange={(e) => setLimitAsset(e.target.value)} required />
                  <Input label="Daily Limit" value={limitValue} onChange={(e) => setLimitValue(e.target.value)} required />
                  <Button type="submit">Set Limit</Button>
                </form>
              ),
            },
          ]}
        />
      </div>
    </Layout>
  );
}
