import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { listAPIKeys, createAPIKey, revokeAPIKey, getPnL, getPnLHistory } from '@/api/account';
import { useAuthStore } from '@/store/authStore';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatDate, formatPrice, changeColorClass } from '@/utils/format';
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
    try {
      const k = await createAPIKey(name || t('account.apiKeyDefaultName'));
      setNewKey(k.key || null);
      setName('');
      refetch();
    } catch { /* ignore */ }
  };

  const pnlValue = (v?: string) => parseFloat(v || '0');

  const totalPnL = pnlValue(pnl?.total_realized) + pnlValue(pnl?.unrealized);
  const portfolioValue = pnlValue(pnl?.portfolio_value);
  const roi = portfolioValue > 0 ? (totalPnL / portfolioValue) * 100 : 0;

  const maxHist = Math.max(1, ...history.map((h) => Math.abs(pnlValue(h.realized))));

  return (
    <Layout>
      <div className="grid h-full grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        <Card title={t('account.portfolio')} className="lg:col-span-1">
          <div className="space-y-3 p-4">
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.portfolioValue')}</span>
              <span className="font-mono text-nexa-100">{formatPrice(pnl?.portfolio_value, 2)} USDT</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.totalPnL')}</span>
              <span className={changeColorClass(totalPnL)}>{formatPrice(totalPnL.toString(), 2)} USDT</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.roi')}</span>
              <span className={changeColorClass(roi)}>{formatPrice(roi, 2)}%</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.totalFees')}</span>
              <span className="text-down">{formatPrice(pnl?.total_fees, 2)} USDT</span>
            </div>
          </div>
        </Card>

        <Card title={t('account.pnlToday')} className="lg:col-span-1">
          <div className="space-y-3 p-4">
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.todayRealized')}</span>
              <span className={changeColorClass(pnlValue(pnl?.today_realized))}>{formatPrice(pnl?.today_realized, 2)} USDT</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.totalRealized')}</span>
              <span className={changeColorClass(pnlValue(pnl?.total_realized))}>{formatPrice(pnl?.total_realized, 2)} USDT</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-nexa-400">{t('account.unrealized')}</span>
              <span className={changeColorClass(pnlValue(pnl?.unrealized))}>{formatPrice(pnl?.unrealized, 2)} USDT</span>
            </div>
          </div>
        </Card>

        <Card title={t('account.positions')} className="lg:col-span-1">
          <div className="max-h-56 space-y-1 overflow-auto p-4">
            {(pnl?.positions || []).length === 0 && (
              <div className="text-sm text-nexa-500">{t('account.noPositions')}</div>
            )}
            {(pnl?.positions || []).map((p) => (
              <div key={p.asset} className="flex justify-between text-xs">
                <span className="text-nexa-300">{p.asset}</span>
                <span className="font-mono text-nexa-100">
                  {formatPrice(p.qty, 6)} @ {formatPrice(p.avg_cost, 2)}
                </span>
              </div>
            ))}
          </div>
        </Card>

        <Card title={t('account.realizedHistory')} className="lg:col-span-2">
          <div className="space-y-3 p-4">
            <div className="flex gap-2">
              {[7, 30].map((d) => (
                <Button
                  key={d}
                  size="sm"
                  variant={historyDays === d ? 'primary' : 'secondary'}
                  onClick={() => setHistoryDays(d)}
                >
                  {d}D
                </Button>
              ))}
            </div>
            {history.length === 0 ? (
              <div className="text-sm text-nexa-500">{t('account.noHistory')}</div>
            ) : (
              <div className="flex h-40 items-end gap-1">
                {history.map((h) => {
                  const v = pnlValue(h.realized);
                  const hgt = `${(Math.abs(v) / maxHist) * 80 + 5}%`;
                  return (
                    <div key={h.date} className="group relative flex flex-1 flex-col items-center justify-end">
                      <div
                        className={`w-full rounded-t ${v >= 0 ? 'bg-up' : 'bg-down'}`}
                        style={{ height: hgt, minHeight: 4 }}
                      />
                      <div className="mt-1 text-[10px] text-nexa-500">{h.date.slice(5)}</div>
                      <div className="pointer-events-none absolute -top-8 left-1/2 z-10 hidden -translate-x-1/2 whitespace-nowrap rounded bg-nexa-900 px-2 py-1 text-xs text-nexa-100 shadow group-hover:block">
                        {h.date}: {formatPrice(h.realized, 2)} USDT
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Card>

        <Card title={t('account.profile')} className="lg:col-span-1">
          <div className="space-y-2 p-4">
            <div className="text-sm text-nexa-400">{t('account.email')}</div>
            <div className="text-nexa-100">{user?.email}</div>
            <div className="text-sm text-nexa-400">{t('account.role')}</div>
            <div className="text-nexa-100"><Badge color="accent">{user?.role}</Badge></div>
          </div>
        </Card>

        <Card title={t('account.apiKeys')} className="lg:col-span-3">
          <div className="space-y-3 p-4">
            <div className="flex gap-2">
              <Input placeholder={t('account.keyName')} value={name} onChange={(e) => setName(e.target.value)} />
              <Button onClick={create}>{t('account.create')}</Button>
            </div>
            {newKey && (
              <div className="rounded border border-accent/30 bg-accent/10 p-2 text-xs text-accent">
                {t('account.saveKeyWarning')}<br />{newKey}
              </div>
            )}
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400"><tr><th className="py-2">{t('account.name')}</th><th className="py-2">{t('account.created')}</th><th></th></tr></thead>
              <tbody>
                {(keys || []).map((k: APIKey) => (
                  <tr key={k.id} className="border-b border-nexa-700/50">
                    <td className="py-2">{k.name}</td>
                    <td className="py-2 text-nexa-400">{formatDate(k.created_at)}</td>
                    <td className="py-2"><Button size="sm" variant="danger" onClick={() => revokeAPIKey(k.id).then(refetch)}>{t('account.revoke')}</Button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    </Layout>
  );
}
