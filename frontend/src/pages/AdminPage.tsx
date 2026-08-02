import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Badge } from '@/components/common/Badge';
import { Input } from '@/components/common/Input';
import {
  listWithdrawals, approveWithdrawal, rejectWithdrawal,
  listPairRisk, updatePairRisk, setUserDailyLimit,
  seedAmmPools, startAmmSimulator, stopAmmSimulator, getAmmSimulatorStatus,
  type AmmSimulatorStatus,
} from '@/api/admin';
import { getAmmPools } from '@/api/amm';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatDate, formatQty } from '@/utils/format';
import type { WithdrawalReviewItem, PairRiskConfig } from '@/types';

export function AdminPage() {
  const { t } = useTranslation();
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
      <div className="h-full overflow-y-auto p-4">
        <Tabs
          defaultTab="withdrawals"
          tabs={[
            {
              id: 'withdrawals',
              label: t('admin.withdrawalReview'),
              content: (
                <div className="overflow-auto">
                  <table className="w-full text-left text-sm">
                    <thead className="text-nexa-400">
                      <tr><th className="py-2">ID</th><th className="py-2">{t('admin.userId')}</th><th className="py-2">{t('admin.asset')}</th><th className="py-2">{t('wallet.amount')}</th><th className="py-2">{t('trading.status')}</th><th className="py-2">{t('trading.time')}</th><th></th></tr>
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
                              <Button size="sm" variant="success" onClick={() => approveWithdrawal(w.id).then(refetchW)}>{t('admin.approve')}</Button>
                              <Button size="sm" variant="danger" onClick={() => rejectWithdrawal(w.id).then(refetchW)}>{t('admin.reject')}</Button>
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
              label: t('admin.riskControls'),
              content: (
                <div className="space-y-3">
                  {(riskPairs?.pairs || []).map((cfg: PairRiskConfig) => (
                    <div key={cfg.pair} className="rounded border border-nexa-700 bg-nexa-900 p-3">
                      <div className="mb-2 flex items-center justify-between">
                        <span className="font-medium text-nexa-100">{cfg.pair}</span>
                        <Badge color={cfg.trading_enabled ? 'up' : 'down'}>{cfg.trading_enabled ? t('admin.running') : t('admin.stopped')}</Badge>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button size="sm" variant="secondary" onClick={() => updateRisk(cfg.pair, { trading_enabled: false })}>{t('admin.pause')}</Button>
                        <Button size="sm" variant="success" onClick={() => updateRisk(cfg.pair, { trading_enabled: true })}>{t('admin.resume')}</Button>
                        <Button size="sm" variant="secondary" onClick={() => updateRisk(cfg.pair, { market_orders_enabled: !cfg.market_orders_enabled })}>
                          {cfg.market_orders_enabled ? t('admin.disableMarket') : t('admin.enableMarket')}
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              ),
            },
            {
              id: 'limits',
              label: t('admin.userLimits'),
              content: (
                <form
                  onSubmit={async (e) => {
                    e.preventDefault();
                    await setUserDailyLimit(userId, limitAsset, limitValue);
                    setLimitValue('');
                  }}
                  className="max-w-md space-y-3"
                >
                  <Input label={t('admin.userId')} value={userId} onChange={(e) => setUserId(e.target.value)} required />
                  <Input label={t('admin.asset')} value={limitAsset} onChange={(e) => setLimitAsset(e.target.value)} required />
                  <Input label={t('admin.dailyLimit')} value={limitValue} onChange={(e) => setLimitValue(e.target.value)} required />
                  <Button type="submit">{t('admin.setLimit')}</Button>
                </form>
              ),
            },
            {
              id: 'amm',
              label: t('admin.ammPools'),
              content: <AmmAdminPanel />,
            },
          ]}
        />
      </div>
    </Layout>
  );
}

// AmmAdminPanel combines pool management (seed) and simulator control. Admins use
// this to bring up a fresh market, pause/resume price simulation, and watch the
// per-pair mid price the feed currently reports.
function AmmAdminPanel() {
  const { t } = useTranslation();
  const { data: pools, refetch: refetchPools } = useFetch(getAmmPools, []);
  const { data: sim, refetch: refetchSim } = useFetch(getAmmSimulatorStatus, []);
  usePolling(() => { refetchSim(); refetchPools(); }, 3000);

  const [busy, setBusy] = useState<'seed' | 'start' | 'stop' | null>(null);
  const [msg, setMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

  const run = async (op: 'seed' | 'start' | 'stop', fn: () => Promise<unknown>) => {
    setBusy(op);
    setMsg(null);
    try {
      const res = await fn() as AmmSimulatorStatus & { pairs?: string[] };
      if (op === 'seed' && res?.pairs) {
        setMsg({ text: t('admin.seeded', { count: res.pairs.length }), type: 'success' });
      } else {
        setMsg({ text: res?.running ? t('admin.running') : t('admin.stopped'), type: 'success' });
      }
      refetchSim();
      refetchPools();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : String(err), type: 'error' });
    } finally {
      setBusy(null);
    }
  };

  const configured = sim?.configured !== false;
  const running = !!sim?.running;
  const prices = sim?.prices || {};

  return (
    <div className="space-y-6">
      <section className="rounded border border-nexa-700 bg-nexa-900 p-4">
        <h3 className="mb-3 text-sm font-medium text-nexa-100">{t('admin.simulatorStatus')}</h3>
        {!configured ? (
          <div className="text-sm text-nexa-500">{t('admin.notConfigured')}</div>
        ) : (
          <>
            <div className="mb-3 flex flex-wrap items-center gap-3">
              <Badge color={running ? 'up' : 'down'}>{running ? t('admin.running') : t('admin.stopped')}</Badge>
              {typeof sim?.interval_ms === 'number' && (
                <span className="text-xs text-nexa-400">{t('admin.interval')}: {(sim.interval_ms / 1000).toFixed(1)}s</span>
              )}
              <div className="flex gap-2">
                <Button size="sm" variant="success" isLoading={busy === 'start'} disabled={running} onClick={() => run('start', startAmmSimulator)}>{t('admin.start')}</Button>
                <Button size="sm" variant="danger" isLoading={busy === 'stop'} disabled={!running} onClick={() => run('stop', stopAmmSimulator)}>{t('admin.stop')}</Button>
              </div>
            </div>
            <div className="mb-3">
              <Button size="sm" variant="secondary" isLoading={busy === 'seed'} onClick={() => run('seed', seedAmmPools)}>{t('admin.seedPools')}</Button>
            </div>
            {msg && (
              <div className={`mb-3 rounded border px-3 py-2 text-sm ${msg.type === 'success' ? 'border-up/20 bg-up/10 text-up' : 'border-down/20 bg-down/10 text-down'}`}>
                {msg.text}
              </div>
            )}
            <div>
              <div className="mb-2 text-xs text-nexa-500">{t('admin.currentPrices')}</div>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-5">
                {Object.keys(prices).length === 0 && <div className="text-xs text-nexa-500">--</div>}
                {Object.entries(prices).map(([pair, price]) => (
                  <div key={pair} className="rounded border border-nexa-800 bg-nexa-950/50 p-2">
                    <div className="text-xs text-nexa-500">{pair}</div>
                    <div className="font-mono text-sm text-nexa-100">{formatPrice(price, 4)}</div>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </section>

      <section className="rounded border border-nexa-700 bg-nexa-900 p-4">
        <h3 className="mb-3 text-sm font-medium text-nexa-100">{t('admin.ammPools')}</h3>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-nexa-400">
              <tr>
                <th className="py-2">{t('markets.pair')}</th>
                <th className="py-2">{t('amm.price')}</th>
                <th className="py-2">{t('amm.reserve')} 0</th>
                <th className="py-2">{t('amm.reserve')} 1</th>
                <th className="py-2">{t('amm.feeRate')}</th>
              </tr>
            </thead>
            <tbody>
              {(pools || []).map((p) => {
                const r0 = parseFloat(p.reserve0 || '0');
                const r1 = parseFloat(p.reserve1 || '0');
                const price = r0 > 0 ? r1 / r0 : 0;
                return (
                  <tr key={p.id} className="border-b border-nexa-700/50">
                    <td className="py-2 font-medium text-nexa-100">{p.pair}</td>
                    <td className="py-2 font-mono">{formatPrice(price, 4)}</td>
                    <td className="py-2 font-mono">{formatQty(p.reserve0, 4)}</td>
                    <td className="py-2 font-mono">{formatQty(p.reserve1, 4)}</td>
                    <td className="py-2 font-mono">{(parseFloat(p.fee_rate || '0') * 100).toFixed(2)}%</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
