import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Tabs } from '@/components/common/Tabs';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { Select } from '@/components/common/Select';
import { StatCard } from '@/components/common/StatCard';
import { EmptyState } from '@/components/common/EmptyState';
import { getBalances, getTransactions, withdraw, getDepositAddress, getSupportedAssets, submitDepositClaim, getDepositClaims } from '@/api/wallet';
import { TransferModal, ACCOUNT_TYPES } from '@/components/common/TransferModal';
import { getKycStatus } from '@/api/kyc';
import { getTickers } from '@/api/market';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatUsd, formatQty, formatDate, changeColorClass, cls } from '@/utils/format';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { toast } from '@/store/toastStore';
import type { AccountType, DepositClaim, DepositClaimStatus, Ticker } from '@/types';

// Stablecoins that are always treated as 1 USD for valuation.
const STABLECOINS = new Set(['USDT', 'USDC', 'DAI', 'BUSD', 'TUSD', 'FRAX']);

// Deposit-claim screenshot constraints (same rules as KYC document uploads).
const MAX_SHOT_BYTES = 5 * 1024 * 1024;
const ACCEPTED_SHOT_TYPES = ['image/jpeg', 'image/png'];

/**
 * Best-effort USD valuation of a balance row using the latest known ticker
 * prices.  Returns null when we have no price (we don't pretend to know).
 */
function valueUsd(row: { asset: string; balance: string }, tickers: Ticker[]): number | null {
  const amt = parseFloat(row.balance || '0');
  if (!Number.isFinite(amt)) return null;
  if (amt === 0) return 0;
  const a = row.asset.toUpperCase();
  if (STABLECOINS.has(a)) return amt;
  // Direct quote against USDT
  const direct = tickers.find((t) => t.pair === `${a}/USDT` || t.pair === `${a}/USDC`);
  if (direct && direct.last) {
    const v = parseFloat(direct.last);
    if (Number.isFinite(v)) return amt * v;
  }
  // Reverse quote: USDT/X → 1 / price
  const reverse = tickers.find((t) => t.pair === `USDT/${a}` || t.pair === `USDC/${a}`);
  if (reverse && reverse.last) {
    const v = parseFloat(reverse.last);
    if (Number.isFinite(v) && v > 0) return amt / v;
  }
  return null;
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
      toast.success(t('wallet.copied'));
    } catch {
      toast.error(t('common.failed'));
    }
  };
  return (
    <Button size="sm" variant="ghost" onClick={copy} icon={
      <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
        {copied
          ? <path d="M5 13l4 4L19 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
          : <><rect x="9" y="9" width="11" height="11" rx="2" stroke="currentColor" strokeWidth="2" /><path d="M5 15V5a2 2 0 012-2h10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" /></>
        }
      </svg>
    }>
      {copied ? t('wallet.copied') : t('wallet.copy')}
    </Button>
  );
}

/**
 * Real-deposit proof form: asset + amount + on-chain txid (required) +
 * optional transfer screenshot (jpg/png ≤5 MB). Submits to
 * POST /wallet/deposit/claim and reports backend errors verbatim
 * (e.g. 409 duplicate txid).
 */
function DepositClaimForm({ assets, asset, onAssetChange, onSubmitted }: {
  assets: string[];
  asset: string;
  onAssetChange: (a: string) => void;
  onSubmitted: () => void;
}) {
  const { t } = useTranslation();
  const [amount, setAmount] = useState('');
  const [txid, setTxid] = useState('');
  const [screenshot, setScreenshot] = useState('');
  const [shotError, setShotError] = useState('');
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const pick = (file: File | undefined) => {
    if (!file) return;
    if (file.size > MAX_SHOT_BYTES) {
      setShotError(t('kyc.fileTooLarge'));
      setScreenshot('');
      return;
    }
    if (!ACCEPTED_SHOT_TYPES.includes(file.type)) {
      setShotError(t('kyc.fileTypeInvalid'));
      setScreenshot('');
      return;
    }
    setShotError('');
    const reader = new FileReader();
    reader.onload = () => setScreenshot(typeof reader.result === 'string' ? reader.result : '');
    reader.readAsDataURL(file);
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    try {
      await submitDepositClaim({ asset, amount, txid: txid.trim(), screenshot: screenshot || undefined });
      toast.success(t('wallet.claimSubmitted'));
      setAmount('');
      setTxid('');
      setScreenshot('');
      setShotError('');
      onSubmitted();
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
      toast.error(msg || (err instanceof Error ? err.message : t('wallet.claimSubmitFailed')));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-3 rounded-lg border border-nexa-700/70 bg-nexa-900/60 p-3">
      <div className="text-xs font-medium text-nexa-200">{t('wallet.claimTitle')}</div>
      <Select label={t('wallet.asset')} value={asset} onChange={(e) => onAssetChange(e.target.value)}>
        {assets.map((a) => <option key={a} value={a}>{a}</option>)}
      </Select>
      <Input
        label={t('wallet.claimAmount')}
        type="number"
        step="0.000001"
        min="0"
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
        required
        suffix={asset}
      />
      <Input
        label={t('wallet.claimTxid')}
        value={txid}
        onChange={(e) => setTxid(e.target.value)}
        required
        placeholder="0x…"
      />
      {/* Optional transfer screenshot */}
      <div>
        <div className="mb-1 block text-xs font-medium text-nexa-300">{t('wallet.claimScreenshot')}</div>
        <input
          ref={fileRef}
          type="file"
          accept="image/jpeg,image/png"
          className="hidden"
          onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ''; }}
        />
        {screenshot ? (
          <div className="relative inline-block">
            <img src={screenshot} alt="screenshot" className="h-24 rounded-lg border border-nexa-700 object-contain" />
            <button
              type="button"
              onClick={() => { setScreenshot(''); setShotError(''); }}
              aria-label={t('wallet.claimRemove')}
              className="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-nexa-700 text-nexa-200 transition-colors hover:bg-down hover:text-white"
            >
              <svg viewBox="0 0 24 24" fill="none" className="h-3 w-3">
                <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className="flex h-24 w-full items-center justify-center rounded-lg border border-dashed border-nexa-600 bg-nexa-900 text-xs text-nexa-500 transition-colors hover:border-accent"
          >
            {t('kyc.uploadHint')}
          </button>
        )}
        {shotError && <p className="mt-1 text-xs text-down">{shotError}</p>}
        <p className="mt-1 text-xs text-nexa-500">{t('kyc.uploadRules')}</p>
      </div>
      <Button type="submit" isLoading={busy} block>
        {t('wallet.claimSubmit')}
      </Button>
    </form>
  );
}

/** Status badge for a deposit claim: pending=黄 / approved=绿 / rejected=红. */
function ClaimStatusBadge({ status }: { status: DepositClaimStatus }) {
  const { t } = useTranslation();
  const map: Record<DepositClaimStatus, { color: 'warning' | 'up' | 'down'; label: string }> = {
    pending: { color: 'warning', label: t('wallet.claimStatusPending') },
    approved: { color: 'up', label: t('wallet.claimStatusApproved') },
    rejected: { color: 'down', label: t('wallet.claimStatusRejected') },
  };
  const m = map[status] || map.pending;
  return <Badge color={m.color}>{m.label}</Badge>;
}

/** The caller's own deposit-claim history, polled like the balance lists. */
function DepositClaimRecords({ claims }: { claims: DepositClaim[] }) {
  const { t } = useTranslation();
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="text-nexa-400">
          <tr>
            <th className="px-3 py-2">{t('wallet.asset')}</th>
            <th className="px-3 py-2 text-right">{t('wallet.amount')}</th>
            <th className="px-3 py-2">{t('wallet.claimTxid')}</th>
            <th className="px-3 py-2">{t('trading.status')}</th>
            <th className="px-3 py-2">{t('kyc.submittedAt')}</th>
          </tr>
        </thead>
        <tbody>
          {claims.length === 0 && (
            <tr>
              <td colSpan={5}>
                <EmptyState title={t('wallet.claimEmpty')} compact />
              </td>
            </tr>
          )}
          {claims.map((c) => (
            <tr key={c.id} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
              <td className="px-3 py-2.5 font-medium">{c.asset}</td>
              <td className="px-3 py-2.5 text-right font-mono text-up">+{formatQty(c.amount, 8)}</td>
              <td className="px-3 py-2.5">
                <div className="flex items-center gap-1">
                  <span className="font-mono text-xs text-nexa-300" title={c.txid}>
                    {c.txid.slice(0, 10)}…{c.txid.length > 14 ? c.txid.slice(-4) : ''}
                  </span>
                  <CopyButton text={c.txid} />
                </div>
              </td>
              <td className="px-3 py-2.5">
                <div className="flex flex-col items-start gap-0.5">
                  <ClaimStatusBadge status={c.status} />
                  {c.status === 'rejected' && c.reject_reason && (
                    <span className="max-w-[16rem] truncate text-xs text-down" title={c.reject_reason}>
                      {c.reject_reason}
                    </span>
                  )}
                </div>
              </td>
              <td className="px-3 py-2.5 text-nexa-400">{formatDate(c.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function WalletPage() {
  const { t } = useTranslation();
  const { data: balances, refetch: refetchBalances } = useFetch(getBalances, []);
  const { data: txs, refetch: refetchTxs } = useFetch(getTransactions, []);
  const { data: supportedAssets } = useFetch(getSupportedAssets, []);
  const { data: tickersData } = useFetch(getTickers, []);
  const { data: kycStatus } = useFetch(getKycStatus, []);
  const { data: depositClaims, refetch: refetchClaims } = useFetch(getDepositClaims, []);
  const kycVerified = (kycStatus?.kyc_level || 0) > 0 || kycStatus?.submission?.status === 'approved';
  const tickers: Ticker[] = tickersData?.tickers || [];

  usePolling(() => { refetchBalances(); refetchTxs(); refetchClaims(); }, 5000);

  const assets = useMemo(() => {
    const set = new Set<string>(supportedAssets || []);
    set.add('USDT');
    SUPPORTED_PAIRS.forEach((p) => { const base = p.split('/')[0]; if (base) set.add(base); });
    (balances || []).forEach((b) => set.add(b.asset));
    return Array.from(set).sort();
  }, [supportedAssets, balances]);

  const [asset, setAsset] = useState(assets[0] || 'BTC');
  useEffect(() => {
    if (assets.length && !assets.includes(asset)) setAsset(assets[0]);
  }, [assets, asset]);

  // Active sub-account tab (spot / futures / funding) + transfer modal state.
  const [account, setAccount] = useState<AccountType>('spot');
  const [transferOpen, setTransferOpen] = useState(false);

  const [address, setAddress] = useState('');
  const [amount, setAmount] = useState('');

  const [depositAddress, setDepositAddress] = useState('');
  const [depositLoading, setDepositLoading] = useState(false);

  const [txFilter, setTxFilter] = useState<'all' | 'deposit' | 'withdrawal'>('all');
  const [txStatus, setTxStatus] = useState<'all' | 'pending' | 'completed' | 'failed'>('all');

  const selectedBalance = useMemo(
    () => (balances || []).find((b) => b.asset === asset && b.account_type === 'spot'),
    [balances, asset]
  );

  // Rows of the active sub-account.
  const accountBalances = useMemo(
    () => (balances || []).filter((b) => b.account_type === account),
    [balances, account]
  );

  // Estimated USD value per sub-account (shown on each tab).
  const accountValues = useMemo(() => {
    const out: Record<AccountType, number> = { spot: 0, futures: 0, funding: 0 };
    for (const b of balances || []) {
      const v = valueUsd(b, tickers);
      if (v !== null) out[b.account_type] += v;
    }
    return out;
  }, [balances, tickers]);

  // Total estimated portfolio value, valued against USD using best-effort pricing.
  const totalValueUsd = useMemo(() => {
    let total = 0;
    let unknown = false;
    for (const b of balances || []) {
      const v = valueUsd(b, tickers);
      if (v === null) unknown = true;
      else total += v;
    }
    return { total, unknown };
  }, [balances, tickers]);

  const submitWithdraw = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await withdraw({ asset, address, amount });
      toast.success(t('wallet.withdrawalSubmitted'));
      setAddress('');
      setAmount('');
      refetchTxs();
      refetchBalances();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('wallet.withdrawalFailed'));
    }
  };

  const fetchDepositAddress = async () => {
    setDepositLoading(true);
    try {
      const res = await getDepositAddress(asset);
      setDepositAddress(res.address || '');
      if (!res.address) toast.info(t('wallet.manualOnly'));
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setDepositLoading(false);
    }
  };

  const filteredTxs = useMemo(() => {
    return (txs || []).filter((tx) => {
      if (txFilter !== 'all' && tx.type !== txFilter) return false;
      if (txStatus !== 'all' && tx.status.toLowerCase() !== txStatus) return false;
      return true;
    });
  }, [txs, txFilter, txStatus]);

  return (
    <Layout>
      <div className="mx-auto grid max-w-6xl grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        {/* Header row: total value + 24h change */}
        <div className="lg:col-span-3 grid grid-cols-1 gap-4 sm:grid-cols-3">
          <StatCard
            label={t('wallet.totalValue')}
            value={formatUsd(totalValueUsd.total.toString(), 2)}
            hint={totalValueUsd.unknown ? t('markets.noData') : t('wallet.totalAcross')}
            tone="accent"
            icon={
              <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
                <path d="M3 17l6-6 4 4 8-8" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M14 7h7v7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            }
          />
          <StatCard
            label={t('wallet.asset')}
            value={`${(balances || []).length}`}
            hint={t('wallet.totalAcross')}
            tone="neutral"
          />
          <StatCard
            label={t('wallet.transactions')}
            value={`${filteredTxs.length}`}
            hint={`${(txs || []).length} ${t('wallet.transactions')}`}
            tone="neutral"
          />
        </div>

        {/* Account tabs (spot / futures / funding) + transfer entry */}
        <div className="lg:col-span-3 flex flex-col gap-3 sm:flex-row sm:items-stretch">
          <div className="flex flex-1 gap-1 rounded-xl border border-nexa-700/70 bg-nexa-800/60 p-1.5 shadow-lg shadow-black/20">
            {ACCOUNT_TYPES.map((acct) => (
              <button
                key={acct}
                type="button"
                onClick={() => setAccount(acct)}
                className={cls(
                  'flex-1 rounded-lg px-3 py-2 text-left transition-all',
                  account === acct
                    ? 'bg-nexa-900/85 ring-1 ring-cta/40'
                    : 'hover:bg-nexa-800/80'
                )}
              >
                <div className={cls('text-sm font-semibold', account === acct ? 'text-cta-bright' : 'text-nexa-300')}>
                  {t(`transfer.account.${acct}`)}
                </div>
                <div className="mt-0.5 font-mono text-xs tabular-nums text-nexa-400">
                  {formatUsd(accountValues[acct].toString(), 2)}
                </div>
              </button>
            ))}
          </div>
          <Button
            onClick={() => setTransferOpen(true)}
            className="sm:self-stretch"
            icon={
              <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
                <path d="M4 7h13m0 0l-3-3m3 3l-3 3M20 17H7m0 0l3-3m-3 3l3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            }
          >
            {t('transfer.title')}
          </Button>
        </div>

        {/* Asset overview */}
        <Card className="lg:col-span-2" title={`${t('wallet.assetOverview')} · ${t(`transfer.account.${account}`)}`}>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-nexa-400">
                <tr>
                  <th className="px-4 py-3">{t('wallet.asset')}</th>
                  <th className="px-4 py-3">{t('wallet.available')}</th>
                  <th className="px-4 py-3">{t('wallet.locked')}</th>
                  <th className="px-4 py-3 text-right">{t('wallet.total')}</th>
                  <th className="px-4 py-3 text-right">{t('wallet.totalValue')}</th>
                </tr>
              </thead>
              <tbody>
                {accountBalances.length === 0 && (
                  <tr>
                    <td colSpan={5}>
                      <EmptyState
                        title={t('wallet.noBalances')}
                        description={t('wallet.simulatedHint')}
                        compact
                      />
                    </td>
                  </tr>
                )}
                {accountBalances.map((b) => {
                  const usd = valueUsd(b, tickers);
                  return (
                    <tr
                      key={b.asset}
                      className={cls(
                        'cursor-pointer border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30',
                        b.asset === asset && 'bg-nexa-800/40'
                      )}
                      onClick={() => setAsset(b.asset)}
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-accent/30 to-cta-deep/30 text-xs font-bold text-accent">
                            {b.asset.slice(0, 1)}
                          </span>
                          <span className="font-medium">{b.asset}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3 font-mono">{formatQty(b.available, 8)}</td>
                      <td className="px-4 py-3 font-mono text-nexa-400">{formatQty(b.locked, 8)}</td>
                      <td className="px-4 py-3 text-right font-mono">{formatQty(b.balance, 8)}</td>
                      <td className="px-4 py-3 text-right font-mono text-nexa-300">
                        {usd === null ? '—' : formatUsd(usd.toString(), 2)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>

        {/* Actions */}
        <Card title={t('wallet.actions')}>
          <div className="p-2">
            <Tabs
              defaultTab="deposit"
              tabs={[
                {
                  id: 'deposit',
                  label: t('wallet.deposit'),
                  content: (
                    <div className="space-y-4 pt-2">
                      <Select label={t('wallet.asset')} value={asset} onChange={(e) => { setAsset(e.target.value); setDepositAddress(''); }}>
                        {assets.map((a) => <option key={a} value={a}>{a}</option>)}
                      </Select>
                      {selectedBalance && (
                        <div className="text-xs text-nexa-400">
                          {t('wallet.availableColon')} <span className="font-mono text-nexa-200">{formatQty(selectedBalance.available, 8)} {asset}</span>
                        </div>
                      )}

                      {/* On-chain deposit address */}
                      <div className="space-y-2">
                        <Button onClick={fetchDepositAddress} isLoading={depositLoading} variant="secondary" block>
                          {t('wallet.getDepositAddress')}
                        </Button>
                        {depositAddress && (
                          <div className="space-y-2 rounded-lg border border-nexa-700/70 bg-nexa-900/60 p-3">
                            <div className="text-xs text-nexa-500">{t('wallet.onChainAddress')}</div>
                            <div className="break-all font-mono text-sm text-nexa-200">{depositAddress}</div>
                            <CopyButton text={depositAddress} />
                          </div>
                        )}
                        {!depositAddress && !depositLoading && (
                          <div className="text-xs text-nexa-500">{t('wallet.manualOnly')}</div>
                        )}
                      </div>

                      {/* Real-deposit proof submission (txid + optional screenshot) */}
                      <DepositClaimForm
                        assets={assets}
                        asset={asset}
                        onAssetChange={setAsset}
                        onSubmitted={refetchClaims}
                      />

                      {/* Self-service simulated credits were removed: the
                          deposit endpoint is admin-only now (privilege
                          escalation fix). Deposits arrive via on-chain
                          detection; admins can credit from the Admin page. */}
                      <div className="rounded-lg border border-nexa-700/70 bg-nexa-900/60 p-3 text-xs text-nexa-400">
                        {t('wallet.depositNote')}
                      </div>
                    </div>
                  ),
                },
                {
                  id: 'withdraw',
                  label: t('wallet.withdraw'),
                  content: (
                    <form onSubmit={submitWithdraw} className="space-y-4 pt-2">
                      {/* KYC withdrawal-limit notice */}
                      {kycVerified ? (
                        <div className="flex items-center gap-2 rounded-lg border border-up/25 bg-up/10 px-3 py-2 text-xs text-up">
                          <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5 flex-shrink-0">
                            <path d="M5 13l4 4L19 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                          {t('wallet.kycVerifiedNote')}
                        </div>
                      ) : (
                        <div className="rounded-lg border border-cta/30 bg-cta/10 px-3 py-2 text-xs text-cta">
                          {t('wallet.kycWithdrawNotice')}
                          <Link to="/kyc" className="ml-1 font-semibold underline underline-offset-2 hover:text-cta-bright">
                            {t('kyc.verifyCta')}
                          </Link>
                        </div>
                      )}
                      <Select label={t('wallet.asset')} value={asset} onChange={(e) => setAsset(e.target.value)}>
                        {assets.map((a) => <option key={a} value={a}>{a}</option>)}
                      </Select>
                      {selectedBalance && (
                        <div className="text-xs text-nexa-400">
                          {t('wallet.availableColon')} <span className="font-mono text-nexa-200">{formatQty(selectedBalance.available, 8)} {asset}</span>
                        </div>
                      )}
                      <Input
                        label={t('wallet.destinationAddress')}
                        value={address}
                        onChange={(e) => setAddress(e.target.value)}
                        required
                        placeholder="0x…"
                      />
                      <Input
                        label={t('wallet.amount')}
                        type="number"
                        step="0.000001"
                        value={amount}
                        onChange={(e) => setAmount(e.target.value)}
                        required
                        suffix={asset}
                      />
                      <Button type="submit" variant="danger" block>
                        {t('wallet.withdraw')}
                      </Button>
                    </form>
                  ),
                },
              ]}
            />
          </div>
        </Card>

        {/* Deposit claim history (凭证提交后的审核进度) */}
        <Card className="lg:col-span-3" title={t('wallet.claimRecords')}>
          <div className="p-4">
            <DepositClaimRecords claims={depositClaims || []} />
          </div>
        </Card>

        {/* Transactions */}
        <Card className="lg:col-span-3" title={t('wallet.transactionHistory')}>
          <div className="space-y-3 p-4">
            <div className="flex flex-wrap gap-3">
              <Select label="" value={txFilter} onChange={(e) => setTxFilter(e.target.value as typeof txFilter)} className="w-36">
                <option value="all">{t('wallet.allTypes')}</option>
                <option value="deposit">{t('wallet.deposit')}</option>
                <option value="withdrawal">{t('wallet.withdraw')}</option>
              </Select>
              <Select label="" value={txStatus} onChange={(e) => setTxStatus(e.target.value as typeof txStatus)} className="w-36">
                <option value="all">{t('wallet.allStatus')}</option>
                <option value="pending">{t('common.pending')}</option>
                <option value="completed">{t('common.completed')}</option>
                <option value="failed">{t('common.failed')}</option>
              </Select>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-nexa-400">
                  <tr>
                    <th className="px-3 py-2">{t('trading.type')}</th>
                    <th className="px-3 py-2">{t('wallet.asset')}</th>
                    <th className="px-3 py-2 text-right">{t('wallet.amount')}</th>
                    <th className="px-3 py-2">{t('trading.status')}</th>
                    <th className="px-3 py-2">{t('trading.time')}</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTxs.length === 0 && (
                    <tr>
                      <td colSpan={5}>
                        <EmptyState title={t('wallet.noMatchingTx')} compact />
                      </td>
                    </tr>
                  )}
                  {filteredTxs.map((tx) => (
                    <tr key={tx.id} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
                      <td className="px-3 py-2.5">
                        <Badge color={tx.type === 'deposit' ? 'up' : tx.type === 'withdrawal' ? 'down' : 'neutral'}>
                          {tx.type}
                        </Badge>
                      </td>
                      <td className="px-3 py-2.5 font-medium">{tx.asset}</td>
                      <td className={cls('px-3 py-2.5 text-right font-mono', changeColorClass(tx.amount))}>
                        {tx.type === 'withdrawal' ? '−' : '+'}{formatQty(tx.amount, 8)}
                      </td>
                      <td className="px-3 py-2.5">
                        <Badge color={tx.status === 'completed' ? 'up' : tx.status === 'failed' ? 'down' : 'warning'}>
                          {tx.status}
                        </Badge>
                      </td>
                      <td className="px-3 py-2.5 text-nexa-400">{formatDate(tx.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </Card>
      </div>

      {/* Internal transfer between sub-accounts */}
      <TransferModal
        open={transferOpen}
        onClose={() => setTransferOpen(false)}
        balances={balances || []}
        initialFrom={account}
        onSuccess={() => refetchBalances()}
      />
    </Layout>
  );
}
