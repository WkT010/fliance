import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Badge } from '@/components/common/Badge';
import { Input } from '@/components/common/Input';
import {
  listWithdrawals, approveWithdrawal, rejectWithdrawal,
  listPairRisk, updatePairRisk, setUserDailyLimit, adminDeposit,
  seedAmmPools, startAmmSimulator, stopAmmSimulator, getAmmSimulatorStatus,
  listKyc, reviewKyc, fetchKycDoc, getPriceAdjust, setPriceAdjust,
  type AmmSimulatorStatus,
} from '@/api/admin';
import { getAmmPools } from '@/api/amm';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatPrice, formatDate, formatQty } from '@/utils/format';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { toast } from '@/store/toastStore';
import type { WithdrawalReviewItem, PairRiskConfig, KycSubmission, PriceAdjustConfig } from '@/types';

/** Backend timestamps here are Unix nanoseconds; formatDate auto-detects ns. */
const nanoDate = (ts?: number) => (ts ? formatDate(ts) : '--');
const isImageUrl = (v?: string) => !!v && (v.startsWith('data:') || /^https?:\/\//.test(v));

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
      <div className="p-4 pb-8">
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
              id: 'deposit',
              label: t('admin.manualDeposit'),
              content: <ManualDepositPanel />,
            },
            {
              id: 'amm',
              label: t('admin.ammPools'),
              content: <AmmAdminPanel />,
            },
            {
              id: 'kyc',
              label: t('admin.kycReview'),
              content: <KycReviewPanel />,
            },
            {
              id: 'priceAdjust',
              label: t('admin.priceAdjust'),
              content: <PriceAdjustPanel />,
            },
          ]}
        />
      </div>
    </Layout>
  );
}

// ManualDepositPanel credits balances via the admin-only deposit endpoint
// (POST /admin/wallet/deposit) — the successor of the removed self-service
// POST /wallet/deposit. The backend credits the caller's own account.
function ManualDepositPanel() {
  const { t } = useTranslation();
  const [asset, setAsset] = useState('USDT');
  const [amount, setAmount] = useState('');
  const [txHash, setTxHash] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await adminDeposit(asset, amount, txHash || undefined);
      toast.success(t('wallet.depositSuccess', { amount: formatQty(amount, 8), asset }));
      setAmount('');
      setTxHash('');
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('wallet.depositFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="max-w-md space-y-3">
      <p className="text-xs text-nexa-500">{t('admin.depositHint')}</p>
      <Input label={t('admin.asset')} value={asset} onChange={(e) => setAsset(e.target.value)} required />
      <Input label={t('wallet.amount')} type="number" step="0.000001" value={amount} onChange={(e) => setAmount(e.target.value)} required />
      <Input label={t('admin.txHash')} value={txHash} onChange={(e) => setTxHash(e.target.value)} />
      <Button type="submit" isLoading={busy}>{t('wallet.confirmDeposit')}</Button>
    </form>
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

// KycReviewPanel lists pending identity submissions and lets admins approve
// or reject them. Reject requires a reason entered via a modal.
function KycReviewPanel() {
  const { t } = useTranslation();
  const { data, refetch } = useFetch(() => listKyc('pending'), []);
  usePolling(refetch, 10000);

  const [busyId, setBusyId] = useState('');
  const [rejecting, setRejecting] = useState<KycSubmission | null>(null);
  const [reason, setReason] = useState('');
  const [lightbox, setLightbox] = useState('');
  const [docUrls, setDocUrls] = useState<Record<string, string>>({});

  // Documents submitted from the web app arrive as filesystem paths on the
  // gateway host; resolve them through the admin-only documents endpoint so
  // the review table shows the actual scan (data: URLs render directly).
  useEffect(() => {
    const subs = data?.submissions || [];
    let cancelled = false;
    subs.forEach((s) => {
      (['front', 'back'] as const).forEach((side) => {
        const doc = side === 'front' ? s.doc_front : s.doc_back;
        const key = `${s.id}:${side}`;
        if (!doc || isImageUrl(doc) || docUrls[key]) return;
        fetchKycDoc(s.id, side)
          .then((url) => {
            if (!cancelled) setDocUrls((prev) => (prev[key] ? prev : { ...prev, [key]: url }));
          })
          .catch(() => { /* path shown as fallback */ });
      });
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  const act = async (id: string, action: 'approve' | 'reject', r?: string) => {
    setBusyId(id);
    try {
      await reviewKyc(id, action, r);
      toast.success(action === 'approve' ? t('admin.kycApproved') : t('admin.kycRejected'));
      refetch();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('admin.kycActionFailed'));
    } finally {
      setBusyId('');
    }
  };

  const docCell = (s: KycSubmission, side: 'front' | 'back') => {
    const doc = side === 'front' ? s.doc_front : s.doc_back;
    const src = isImageUrl(doc) ? doc : docUrls[`${s.id}:${side}`];
    return src ? (
      <img
        src={src}
        alt="doc"
        className="h-10 w-14 cursor-zoom-in rounded border border-nexa-700 object-cover"
        onClick={() => setLightbox(src)}
      />
    ) : (
      <span className="break-all font-mono text-[10px] text-nexa-500">{(doc || '').slice(0, 28) || '--'}</span>
    );
  };

  const rows = data?.submissions || [];

  return (
    <div className="space-y-4">
      {rows.length === 0 && (
        <div className="rounded border border-nexa-700 bg-nexa-900 p-6 text-center text-sm text-nexa-500">
          {t('admin.kycEmpty')}
        </div>
      )}
      {rows.length > 0 && (
        <div className="overflow-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-nexa-400">
              <tr>
                <th className="py-2">{t('admin.userId')}</th>
                <th className="py-2">{t('admin.kycName')}</th>
                <th className="py-2">{t('admin.kycIdNumber')}</th>
                <th className="py-2">{t('admin.kycDocs')}</th>
                <th className="py-2">{t('kyc.submittedAt')}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id} className="border-b border-nexa-700/50 align-top">
                  <td className="py-2">{s.user_id}</td>
                  <td className="py-2">{s.full_name}</td>
                  <td className="py-2 font-mono text-xs">{s.id_number}</td>
                  <td className="py-2">
                    <div className="flex items-center gap-1.5">
                      {docCell(s, 'front')}
                      {docCell(s, 'back')}
                    </div>
                  </td>
                  <td className="py-2 text-nexa-400">{nanoDate(s.submitted_at)}</td>
                  <td className="py-2">
                    <div className="flex gap-2">
                      <Button size="sm" variant="success" disabled={busyId === s.id} onClick={() => act(s.id, 'approve')}>
                        {t('admin.approve')}
                      </Button>
                      <Button size="sm" variant="danger" disabled={busyId === s.id} onClick={() => { setRejecting(s); setReason(''); }}>
                        {t('admin.reject')}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Reject-reason modal */}
      {rejecting && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setRejecting(null)}>
          <div className="w-full max-w-md rounded-lg border border-nexa-700 bg-nexa-900 p-5" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-sm font-medium text-nexa-100">{t('admin.kycRejectTitle')}</h3>
            <Input label={t('admin.kycRejectReason')} value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setRejecting(null)}>{t('common.cancel')}</Button>
              <Button
                variant="danger"
                disabled={!reason.trim()}
                onClick={async () => {
                  await act(rejecting.id, 'reject', reason.trim());
                  setRejecting(null);
                }}
              >
                {t('admin.reject')}
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Document lightbox */}
      {lightbox && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6" onClick={() => setLightbox('')}>
          <img src={lightbox} alt="doc" className="max-h-full max-w-full rounded object-contain" />
        </div>
      )}
    </div>
  );
}

// PriceAdjustPanel lets operators tune the per-pair price multiplier/offset
// used by the market feed. Changes propagate to futures mark price & settlement.
function PriceAdjustPanel() {
  const { t } = useTranslation();
  const [configs, setConfigs] = useState<Record<string, PriceAdjustConfig>>({});
  const [edits, setEdits] = useState<Record<string, { multiplier: string; offset: string }>>({});
  const [saving, setSaving] = useState('');
  const [loaded, setLoaded] = useState(false);

  const load = async () => {
    const results = await Promise.all(
      SUPPORTED_PAIRS.map((p) => getPriceAdjust(p).catch(() => null))
    );
    const map: Record<string, PriceAdjustConfig> = {};
    const em: Record<string, { multiplier: string; offset: string }> = {};
    SUPPORTED_PAIRS.forEach((p, i) => {
      const c = results[i] || { pair: p, multiplier: '1', offset: '0', updated_by: '', updated_at: 0 };
      map[p] = c;
      em[p] = { multiplier: c.multiplier, offset: c.offset };
    });
    setConfigs(map);
    setEdits(em);
    setLoaded(true);
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!loaded) {
    return <div className="p-4 text-sm text-nexa-500">{t('common.loading')}</div>;
  }

  const save = async (pair: string) => {
    const e = edits[pair];
    const m = parseFloat(e.multiplier);
    if (!isFinite(m) || m < 0.1 || m > 10) {
      toast.error(t('admin.priceMultiplierRange'));
      return;
    }
    if (!isFinite(parseFloat(e.offset || '0'))) {
      toast.error(t('admin.priceOffsetInvalid'));
      return;
    }
    setSaving(pair);
    try {
      const res = await setPriceAdjust(pair, e.multiplier, e.offset || '0');
      setConfigs((prev) => ({ ...prev, [pair]: res }));
      toast.success(t('admin.priceSaved'));
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
      toast.error(msg || (err instanceof Error ? err.message : t('admin.priceSaveFailed')));
    } finally {
      setSaving('');
    }
  };

  return (
    <div className="space-y-4">
      <div className="rounded border border-warning/30 bg-warning/10 p-3 text-sm text-warning">
        {t('admin.priceAdjustWarning')}
      </div>
      <div className="overflow-auto">
        <table className="w-full text-left text-sm">
          <thead className="text-nexa-400">
            <tr>
              <th className="py-2">{t('markets.pair')}</th>
              <th className="py-2">{t('admin.multiplier')}</th>
              <th className="py-2">{t('admin.offset')}</th>
              <th className="py-2">{t('admin.updatedBy')}</th>
              <th className="py-2">{t('admin.updatedAt')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {SUPPORTED_PAIRS.map((pair) => {
              const c = configs[pair];
              const e = edits[pair];
              return (
                <tr key={pair} className="border-b border-nexa-700/50">
                  <td className="py-2 font-medium text-nexa-100">{pair}</td>
                  <td className="py-2">
                    <input
                      className="w-24 rounded border border-nexa-700 bg-nexa-950 px-2 py-1 font-mono text-sm text-nexa-100 outline-none focus:border-accent"
                      value={e.multiplier}
                      onChange={(ev) => setEdits((prev) => ({ ...prev, [pair]: { ...e, multiplier: ev.target.value } }))}
                    />
                  </td>
                  <td className="py-2">
                    <input
                      className="w-24 rounded border border-nexa-700 bg-nexa-950 px-2 py-1 font-mono text-sm text-nexa-100 outline-none focus:border-accent"
                      value={e.offset}
                      onChange={(ev) => setEdits((prev) => ({ ...prev, [pair]: { ...e, offset: ev.target.value } }))}
                    />
                  </td>
                  <td className="py-2 text-nexa-400">{c.updated_by || '--'}</td>
                  <td className="py-2 text-nexa-400">{nanoDate(c.updated_at)}</td>
                  <td className="py-2">
                    <Button size="sm" variant="secondary" isLoading={saving === pair} onClick={() => save(pair)}>
                      {t('common.save')}
                    </Button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
