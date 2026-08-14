import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { StatCard } from '@/components/common/StatCard';
import { EmptyState } from '@/components/common/EmptyState';
import { listAPIKeys, createAPIKey, revokeAPIKey, getPnL, getPnLHistory } from '@/api/account';
import { getApiErrorMessage } from '@/api/client';
import { useAuthStore } from '@/store/authStore';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatDate, formatPrice, formatUsd, cls, shortAddr } from '@/utils/format';
import { toast } from '@/store/toastStore';
import type { APIKey, PnLSummary, PnLHistoryItem } from '@/types';

export function AccountPage() {
  const { t } = useTranslation();
  const user = useAuthStore((s) => s.user);
  const { data: keys, refetch } = useFetch(listAPIKeys, []);
  const [pnl, setPnL] = useState<PnLSummary | null>(null);
  const [history, setHistory] = useState<PnLHistoryItem[]>([]);
  const [historyDays, setHistoryDays] = useState(7);
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  const loadPnL = async () => {
    try {
      const [summary, hist] = await Promise.all([
        getPnL(),
        getPnLHistory(historyDays),
      ]);
      setPnL(summary);
      setHistory(hist.history || []);
    } catch { /* ignore */ }
  };

  usePolling(loadPnL, 10000);

  const create = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const k = await createAPIKey(name || t('account.apiKeyDefaultName'));
      setNewKey(k.key || null);
      setName('');
      refetch();
      toast.success(t('account.create'));
    } catch (err: unknown) {
      toast.error(getApiErrorMessage(err, t('common.failed')));
    } finally {
      setCreating(false);
    }
  };

  const revoke = async (id: string) => {
    if (revokingId) return;
    setRevokingId(id);
    try {
      await revokeAPIKey(id);
      refetch();
      toast.info(t('account.revoke'));
    } catch (err: unknown) {
      toast.error(getApiErrorMessage(err, t('common.failed')));
    } finally {
      setRevokingId(null);
    }
  };

  const pnlValue = (v?: string) => parseFloat(v || '0');

  const totalPnL = pnlValue(pnl?.total_realized) + pnlValue(pnl?.unrealized);
  const portfolioValue = pnlValue(pnl?.portfolio_value);
  const roi = portfolioValue > 0 ? (totalPnL / portfolioValue) * 100 : 0;
  const maxHist = Math.max(1, ...history.map((h) => Math.abs(pnlValue(h.realized))));

  return (
    <Layout>
      <div className="mx-auto grid max-w-7xl grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        {/* Stat cards */}
        <StatCard
          label={t('account.portfolioValue')}
          value={formatUsd(pnl?.portfolio_value, 2)}
          hint="USDT"
          tone="accent"
        />
        <StatCard
          label={t('account.totalPnL')}
          value={formatUsd(totalPnL.toString(), 2)}
          change={roi}
          tone={totalPnL > 0 ? 'up' : totalPnL < 0 ? 'down' : 'neutral'}
        />
        <StatCard
          label={t('account.unrealized')}
          value={formatUsd(pnl?.unrealized, 2)}
          hint={t('account.totalRealized') + ': ' + formatUsd(pnl?.total_realized, 2)}
          tone={pnlValue(pnl?.unrealized) > 0 ? 'up' : pnlValue(pnl?.unrealized) < 0 ? 'down' : 'neutral'}
        />

        {/* Realized PnL chart */}
        <Card
          className="lg:col-span-2"
          title={t('account.realizedHistory')}
          extra={
            <div className="flex gap-1">
              {[7, 30].map((d) => (
                <Button
                  key={d}
                  size="sm"
                  variant={historyDays === d ? 'primary' : 'ghost'}
                  onClick={() => setHistoryDays(d)}
                >
                  {d}D
                </Button>
              ))}
            </div>
          }
        >
          <div className="space-y-3 p-4">
            {history.length === 0 ? (
              <EmptyState title={t('account.noHistory')} compact />
            ) : (
              <div className="flex h-44 items-end gap-1.5">
                {history.map((h) => {
                  const v = pnlValue(h.realized);
                  const hgt = `${(Math.abs(v) / maxHist) * 88 + 4}%`;
                  return (
                    <div key={h.date} className="group relative flex flex-1 flex-col items-center justify-end">
                      <div
                        className={cls(
                          'w-full rounded-t-md transition-all',
                          v >= 0 ? 'bg-up/80 hover:bg-up' : 'bg-down/80 hover:bg-down'
                        )}
                        style={{ height: hgt, minHeight: 4 }}
                      />
                      <div className="mt-1.5 text-[10px] text-nexa-500">{h.date.slice(5)}</div>
                      <div className="pointer-events-none absolute -top-10 left-1/2 z-10 hidden -translate-x-1/2 whitespace-nowrap rounded-md bg-nexa-900 px-2 py-1 text-xs text-nexa-100 shadow-lg ring-1 ring-nexa-700 group-hover:block">
                        {h.date}: {formatPrice(h.realized, 2)} USDT
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Card>

        {/* Positions */}
        <Card title={t('account.positions')}>
          <div className="max-h-56 space-y-1 overflow-auto p-3">
            {(pnl?.positions || []).length === 0 && (
              <EmptyState title={t('account.noPositions')} compact />
            )}
            {(pnl?.positions || []).map((p) => (
              <div key={p.asset} className="flex items-center justify-between rounded-md border border-nexa-800/40 bg-nexa-900/30 px-3 py-2 text-xs">
                <span className="font-medium text-nexa-100">{p.asset}</span>
                <span className="font-mono text-nexa-300">
                  {formatPrice(p.qty, 6)} <span className="text-nexa-500">@</span> {formatPrice(p.avg_cost, 2)}
                </span>
              </div>
            ))}
          </div>
        </Card>

        {/* Profile */}
        <Card title={t('account.profile')}>
          <div className="space-y-3 p-4">
            <div>
              <div className="text-xs uppercase tracking-wide text-nexa-500">{t('account.email')}</div>
              <div className="mt-1 truncate text-sm text-nexa-100">{user?.email}</div>
            </div>
            <div>
              <div className="text-xs uppercase tracking-wide text-nexa-500">{t('account.role')}</div>
              <div className="mt-1"><Badge color="accent">{user?.role}</Badge></div>
            </div>
          </div>
        </Card>

        {/* API Keys */}
        <Card className="lg:col-span-3" title={t('account.apiKeys')}>
          <div className="space-y-4 p-4">
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                placeholder={t('account.keyName')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="flex-1"
              />
              <Button onClick={create} isLoading={creating} icon={
                <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
                  <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                </svg>
              }>
                {t('account.create')}
              </Button>
            </div>
            {newKey && (
              <div className="rounded-lg border border-accent/40 bg-accent/10 p-3">
                <div className="text-xs font-semibold text-accent">{t('account.saveKeyWarning')}</div>
                <div className="mt-2 break-all rounded bg-nexa-950/60 p-2 font-mono text-xs text-nexa-100">{newKey}</div>
              </div>
            )}
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-nexa-400">
                  <tr>
                    <th className="py-2 font-medium">{t('account.name')}</th>
                    <th className="py-2 font-medium">{t('account.apiKeys')}</th>
                    <th className="py-2 font-medium">{t('account.created')}</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {(keys || []).map((k: APIKey) => (
                    <tr key={k.id} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
                      <td className="py-2.5 font-medium text-nexa-100">{k.name}</td>
                      <td className="py-2.5 font-mono text-xs text-nexa-400">{shortAddr(k.id, 8, 4)}</td>
                      <td className="py-2.5 text-nexa-400">{formatDate(k.created_at)}</td>
                      <td className="py-2.5 text-right">
                        <Button size="sm" variant="danger" isLoading={revokingId === k.id} onClick={() => revoke(k.id)}>
                          {t('account.revoke')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                  {(!keys || keys.length === 0) && (
                    <tr>
                      <td colSpan={4}><EmptyState title={t('account.noHistory')} compact /></td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </Card>
      </div>
    </Layout>
  );
}
