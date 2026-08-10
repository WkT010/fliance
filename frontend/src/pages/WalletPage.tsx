import { useEffect, useMemo, useState } from 'react';
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
import { getBalances, getTransactions, withdraw, getDepositAddress, getSupportedAssets } from '@/api/wallet';
import { getTickers } from '@/api/market';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatUsd, formatQty, formatDate, changeColorClass, cls } from '@/utils/format';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { toast } from '@/store/toastStore';
import type { Ticker } from '@/types';

// Stablecoins that are always treated as 1 USD for valuation.
const STABLECOINS = new Set(['USDT', 'USDC', 'DAI', 'BUSD', 'TUSD', 'FRAX']);

/**
 * Best-effort USD valuation of a balance row using the latest known ticker
 * prices.  Returns null when we have no price (we don't pretend to know).
 */
function valueUsd(balance: { asset: string; total: string }, tickers: Ticker[]): number | null {
  const amt = parseFloat(balance.total || '0');
  if (!Number.isFinite(amt)) return null;
  if (amt === 0) return 0;
  const a = balance.asset.toUpperCase();
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

export function WalletPage() {
  const { t } = useTranslation();
  const { data: balances, refetch: refetchBalances } = useFetch(getBalances, []);
  const { data: txs, refetch: refetchTxs } = useFetch(getTransactions, []);
  const { data: supportedAssets } = useFetch(getSupportedAssets, []);
  const { data: tickersData } = useFetch(getTickers, []);
  const tickers: Ticker[] = tickersData?.tickers || [];

  usePolling(() => { refetchBalances(); refetchTxs(); }, 5000);

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

  const [address, setAddress] = useState('');
  const [amount, setAmount] = useState('');

  const [depositAddress, setDepositAddress] = useState('');
  const [depositLoading, setDepositLoading] = useState(false);

  const [txFilter, setTxFilter] = useState<'all' | 'deposit' | 'withdrawal'>('all');
  const [txStatus, setTxStatus] = useState<'all' | 'pending' | 'completed' | 'failed'>('all');

  const selectedBalance = useMemo(
    () => (balances || []).find((b) => b.asset === asset),
    [balances, asset]
  );

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

        {/* Asset overview */}
        <Card className="lg:col-span-2" title={t('wallet.assetOverview')}>
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
                {(balances || []).length === 0 && (
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
                {(balances || []).map((b) => {
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
                          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-accent/30 to-amber-700/30 text-xs font-bold text-accent">
                            {b.asset.slice(0, 1)}
                          </span>
                          <span className="font-medium">{b.asset}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3 font-mono">{formatQty(b.available, 8)}</td>
                      <td className="px-4 py-3 font-mono text-nexa-400">{formatQty(b.locked, 8)}</td>
                      <td className="px-4 py-3 text-right font-mono">{formatQty(b.total, 8)}</td>
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
    </Layout>
  );
}
