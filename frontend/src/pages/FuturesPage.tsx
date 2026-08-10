import { useState, useMemo, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { ChartPanel } from '@/components/trading/ChartPanel';
import { OrderbookPanel } from '@/components/trading/OrderbookPanel';
import { RecentTrades } from '@/components/trading/RecentTrades';
import { useMarket } from '@/hooks/useMarket';
import { usePolling } from '@/hooks/usePolling';
import { SUPPORTED_PAIRS } from '@/utils/constants';
import { Select } from '@/components/common/Select';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Card } from '@/components/common/Card';
import { Badge } from '@/components/common/Badge';
import { StatCard } from '@/components/common/StatCard';
import { EmptyState } from '@/components/common/EmptyState';
import { formatPrice, formatQty, formatTime, formatUsd, formatPct, cls, changeColorClass } from '@/utils/format';
import { toast } from '@/store/toastStore';
import {
  getMarkPrice,
  getFuturesPositions,
  openFuturesPosition,
  placeFuturesOrder,
  closeFuturesPosition,
  closeFuturesPositionPartial,
  cancelFuturesOrder,
  getFuturesOrders,
  getFuturesAccountSummary,
  addMargin,
  reduceMargin,
  getFundingHistory,
} from '@/api/futures';
import type { FuturesPosition, FuturesOrder, MarkPrice, FuturesSide, MarginMode } from '@/types';

const LEVERAGE_PRESETS = [1, 5, 10, 25, 50, 100];

// Show a price string, treating empty/zero (unset by backend safeFloatStr on
// nil *big.Float) as "not set".
function priceOrDash(v?: string): string {
  return v && v !== '0' ? v : '--';
}

function normalizeTs(ts: number): number {
  if (ts > 1e15) return ts / 1e6;
  return ts;
}

function fundingCountdown(nextFunding: number): string {
  const target = normalizeTs(nextFunding);
  const diff = Math.max(0, Math.floor((target - Date.now()) / 1000));
  const m = Math.floor(diff / 60);
  const s = diff % 60;
  return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
}

export function FuturesPage() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const [pair, setPair] = useState(params.get('pair') || 'BTC/USDT');
  const [side, setSide] = useState<FuturesSide>('long');
  const [marginMode, setMarginMode] = useState<MarginMode>('isolated');
  const [leverage, setLeverage] = useState(20);
  const [orderType, setOrderType] = useState<'market' | 'limit'>('market');
  const [price, setPrice] = useState('');
  const [quantity, setQuantity] = useState('');
  const [tpPrice, setTpPrice] = useState('');
  const [slPrice, setSlPrice] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [markPrice, setMarkPrice] = useState<MarkPrice | null>(null);
  const [positions, setPositions] = useState<FuturesPosition[]>([]);
  const [orders, setOrders] = useState<FuturesOrder[]>([]);
  const [accountSummary, setAccountSummary] = useState<Awaited<ReturnType<typeof getFuturesAccountSummary>> | null>(null);
  const [panelTab, setPanelTab] = useState<'positions' | 'orders'>('positions');
  const [positionTab, setPositionTab] = useState<'open' | 'closed'>('open');
  const [marginEditId, setMarginEditId] = useState<string | null>(null);
  const [marginAmount, setMarginAmount] = useState('');
  const [marginLoading, setMarginLoading] = useState(false);
  const [closeTargetId, setCloseTargetId] = useState<string | null>(null);
  const [closeQty, setCloseQty] = useState('');
  const [closing, setClosing] = useState(false);
  const [closingId, setClosingId] = useState<string | null>(null);
  const [fundingHistory, setFundingHistory] = useState<{ time: number; funding_rate: string; mark_price: string }[]>([]);
  const [countdown, setCountdown] = useState('--:--');

  useMarket(pair);

  const changePair = (p: string) => {
    setPair(p);
    setParams({ pair: p });
  };

  const loadData = async () => {
    try {
      const [mp, pos, summary, ord] = await Promise.all([
        getMarkPrice(pair),
        getFuturesPositions(),
        getFuturesAccountSummary(),
        getFuturesOrders(),
      ]);
      setMarkPrice(mp);
      setPositions(pos);
      setAccountSummary(summary);
      setOrders(ord);
    } catch {
      /* ignore */
    }
  };

  const loadFundingHistory = async () => {
    try {
      const data = await getFundingHistory(pair);
      setFundingHistory(data.history);
    } catch {
      /* ignore */
    }
  };

  usePolling(loadData, 3000, [pair]);
  usePolling(loadFundingHistory, 10000, [pair]);

  useEffect(() => {
    if (!markPrice?.next_funding) return;
    const tick = () => setCountdown(fundingCountdown(markPrice.next_funding));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [markPrice?.next_funding]);

  const mark = parseFloat(markPrice?.mark_price || '0');
  const qtyNum = parseFloat(quantity || '0');

  const estimatedMargin = useMemo(() => {
    if (!mark || !qtyNum) return 0;
    return (mark * qtyNum) / leverage;
  }, [mark, qtyNum, leverage]);

  const estimatedLiq = useMemo(() => {
    if (!mark || !qtyNum) return 0;
    const entry = orderType === 'limit' && price ? parseFloat(price) : mark;
    if (!entry) return 0;
    if (side === 'long') return entry * (1 - 1 / leverage);
    return entry * (1 + 1 / leverage);
  }, [mark, qtyNum, leverage, side, orderType, price]);

  // Available USDT balance from the futures account summary.
  const availableUSDT = useMemo(() => {
    const bal = parseFloat(accountSummary?.wallet_balance || '0');
    const locked = parseFloat(accountSummary?.wallet_locked || '0');
    return Math.max(0, bal - locked);
  }, [accountSummary?.wallet_balance, accountSummary?.wallet_locked]);

  // Max entry quantity given available quote balance and current leverage/price.
  const maxQty = useMemo(() => {
    const entry = orderType === 'limit' && price ? parseFloat(price) : mark;
    if (!entry || entry <= 0 || leverage <= 0) return 0;
    return (availableUSDT * leverage) / entry;
  }, [availableUSDT, leverage, mark, orderType, price]);

  const resetForm = () => {
    setPrice('');
    setQuantity('');
    setTpPrice('');
    setSlPrice('');
  };

  const fillMaxQty = () => {
    if (maxQty > 0) setQuantity(String(maxQty.toFixed(6)));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    if (!quantity || parseFloat(quantity) <= 0) {
      toast.error(t('futures.quantity'));
      return;
    }
    setSubmitting(true);
    try {
      if (orderType === 'market') {
        await openFuturesPosition({
          pair,
          side,
          leverage,
          margin_mode: marginMode,
          quantity,
        });
        toast.success(`${t('futures.open')} ${side.toUpperCase()} ${pair}`, `${quantity} @ ${leverage}x`);
      } else {
        await placeFuturesOrder({
          pair,
          side: side === 'long' ? 'buy' : 'sell',
          type: 'limit',
          quantity,
          price,
          leverage,
          margin_mode: marginMode,
          tp_price: tpPrice || undefined,
          sl_price: slPrice || undefined,
        });
        toast.success(`${t('futures.limit')} ${side.toUpperCase()} ${pair}`, `${quantity} @ ${price}`);
      }
      resetForm();
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleClose = async (id: string) => {
    if (closingId) return;
    setClosingId(id);
    try {
      await closeFuturesPosition(id);
      setCloseTargetId(null);
      setCloseQty('');
      toast.success(t('futures.close'));
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setClosingId(null);
    }
  };

  const handleCancelOrder = async (id: string) => {
    try {
      await cancelFuturesOrder(id);
      toast.info(t('futures.cancel'));
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    }
  };

  const toggleMarginEdit = (id: string) => {
    setMarginEditId((current) => (current === id ? null : id));
    setMarginAmount('');
  };

  const toggleCloseEdit = (id: string) => {
    setCloseTargetId((current) => (current === id ? null : id));
    setCloseQty('');
  };

  const handleAddMargin = async (id: string) => {
    if (!marginAmount || parseFloat(marginAmount) <= 0) return;
    if (marginLoading) return;
    setMarginLoading(true);
    try {
      await addMargin(id, marginAmount);
      setMarginAmount('');
      setMarginEditId(null);
      toast.success(t('futures.add'));
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setMarginLoading(false);
    }
  };

  const handleReduceMargin = async (id: string) => {
    if (!marginAmount || parseFloat(marginAmount) <= 0) return;
    if (marginLoading) return;
    setMarginLoading(true);
    try {
      await reduceMargin(id, marginAmount);
      setMarginAmount('');
      setMarginEditId(null);
      toast.success(t('futures.reduce'));
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setMarginLoading(false);
    }
  };

  const handlePartialClose = async (id: string) => {
    if (!closeQty || parseFloat(closeQty) <= 0) return;
    if (closing) return;
    setClosing(true);
    try {
      await closeFuturesPositionPartial(id, closeQty);
      setCloseTargetId(null);
      setCloseQty('');
      toast.success(t('futures.partialClose'), closeQty);
      await loadData();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setClosing(false);
    }
  };

  const openPositions = positions.filter((p) => p.status === 'open');
  const closedPositions = positions.filter((p) => p.status === 'closed');

  return (
    <Layout>
      <div className="flex flex-col gap-3 p-3 pb-6">
        {/* Header - sticky so it stays visible while scrolling */}
        <div className="sticky top-0 z-30 -mx-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-nexa-700/70 bg-nexa-900/85 px-4 py-3 shadow-lg shadow-black/30 backdrop-blur-xl">
          <div className="flex flex-wrap items-center gap-4">
            <Select
              className="w-40"
              value={pair}
              onChange={(e) => changePair(e.target.value)}
              options={SUPPORTED_PAIRS.map((p) => ({ value: p, label: p }))}
            />
            <div className="flex items-center gap-3">
              <div className="font-mono text-2xl font-bold tabular-nums">
                {formatPrice(markPrice?.mark_price, 2)}
              </div>
              <span className="text-xs text-nexa-500">USDT</span>
            </div>
            <div className="hidden flex-wrap items-center gap-x-5 gap-y-1 text-xs md:flex">
              <div>
                <div className="text-nexa-500">{t('futures.index')}</div>
                <div className="font-mono text-nexa-100">{formatPrice(markPrice?.index_price, 2)}</div>
              </div>
              <div>
                <div className="text-nexa-500">{t('futures.funding')}</div>
                <div className={cls('font-mono font-semibold', changeColorClass(markPrice?.funding_rate))}>
                  {markPrice?.funding_rate ? `${Number(markPrice.funding_rate) >= 0 ? '+' : ''}${(Number(markPrice.funding_rate) * 100).toFixed(4)}%` : '—'}
                </div>
              </div>
              <div>
                <div className="text-nexa-500">{t('futures.next')}</div>
                <div className="font-mono text-nexa-100">{countdown}</div>
              </div>
            </div>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3 lg:grid-cols-12">
          {/* Left: book + trades + funding history */}
          <div className="flex flex-col gap-3 lg:col-span-3">
            <div className="min-h-[420px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <OrderbookPanel pair={pair} compact />
            </div>
            <div className="min-h-[420px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <RecentTrades pair={pair} />
            </div>
            <div className="min-h-[280px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20">
              <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/40 px-4 py-2.5">
                <div className="text-sm font-semibold text-nexa-100">{t('futures.fundingHistory')}</div>
                <Badge color="accent" size="sm">{fundingHistory.length}</Badge>
              </div>
              <div className="max-h-72 overflow-auto p-2">
                {fundingHistory.length === 0 ? (
                  <EmptyState title={t('futures.noFundingHistory')} compact />
                ) : (
                  <table className="w-full text-xs">
                    <thead className="sticky top-0 bg-nexa-800/80 text-nexa-400 backdrop-blur">
                      <tr>
                        <th className="py-1.5 text-left font-medium">{t('futures.time')}</th>
                        <th className="py-1.5 text-right font-medium">{t('futures.rate')}</th>
                        <th className="py-1.5 text-right font-medium">{t('futures.mark')}</th>
                      </tr>
                    </thead>
                    <tbody className="text-nexa-300">
                      {fundingHistory.map((item, idx) => (
                        <tr key={idx} className="border-b border-nexa-800/40 last:border-0 transition-colors hover:bg-nexa-800/40">
                          <td className="py-1.5 font-mono">{formatTime(item.time)}</td>
                          <td className={cls('py-1.5 text-right font-mono font-semibold', changeColorClass(item.funding_rate))}>
                            {item.funding_rate ? `${Number(item.funding_rate) >= 0 ? '+' : ''}${(Number(item.funding_rate) * 100).toFixed(4)}%` : '—'}
                          </td>
                          <td className="py-1.5 text-right font-mono">{formatPrice(item.mark_price, 2)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </div>
          </div>

          {/* Center: chart */}
          <div className="min-h-[520px] overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20 lg:col-span-6">
            <ChartPanel pair={pair} />
          </div>

          {/* Right: account + order + positions */}
          <div className="flex flex-col gap-3 lg:col-span-3">
            <div className="grid grid-cols-2 gap-2">
              <StatCard
                label={t('futures.availableBalance')}
                value={formatUsd(availableUSDT.toString(), 2)}
                tone="accent"
              />
              <StatCard
                label={t('futures.unrealizedPnL')}
                value={formatUsd(accountSummary?.total_pnl, 2)}
                tone={parseFloat(accountSummary?.total_pnl || '0') > 0 ? 'up' : parseFloat(accountSummary?.total_pnl || '0') < 0 ? 'down' : 'neutral'}
              />
            </div>

            <Card title={t('futures.order')}>
              <form onSubmit={handleSubmit} className="space-y-3 p-3">
                <div className="flex gap-1 rounded-lg bg-nexa-900/80 p-1">
                  <button
                    type="button"
                    onClick={() => setSide('long')}
                    className={cls(
                      'flex-1 rounded-md py-2 text-sm font-semibold transition-all',
                      side === 'long'
                        ? 'bg-up text-white shadow-md shadow-up/30'
                        : 'text-nexa-300 hover:bg-nexa-800/60'
                    )}
                  >
                    {t('futures.long')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setSide('short')}
                    className={cls(
                      'flex-1 rounded-md py-2 text-sm font-semibold transition-all',
                      side === 'short'
                        ? 'bg-down text-white shadow-md shadow-down/30'
                        : 'text-nexa-300 hover:bg-nexa-800/60'
                    )}
                  >
                    {t('futures.short')}
                  </button>
                </div>

                <div className="flex items-center justify-between rounded-md border border-nexa-700/50 bg-nexa-900/40 px-3 py-2">
                  <span className="text-xs font-medium text-nexa-300">{t('futures.marginMode')}</span>
                  <div className="flex rounded border border-nexa-700">
                    <button
                      type="button"
                      onClick={() => setMarginMode('isolated')}
                      className={cls(
                        'rounded-l px-3 py-1 text-xs font-medium transition-colors',
                        marginMode === 'isolated' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                      )}
                    >
                      {t('futures.isolated')}
                    </button>
                    <button
                      type="button"
                      onClick={() => setMarginMode('cross')}
                      className={cls(
                        'rounded-r px-3 py-1 text-xs font-medium transition-colors',
                        marginMode === 'cross' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                      )}
                    >
                      {t('futures.cross')}
                    </button>
                  </div>
                </div>

                <div className="rounded-md border border-nexa-700/50 bg-nexa-900/40 p-3">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-medium text-nexa-300">{t('futures.leverage')}</span>
                    <span className="font-mono text-base font-bold text-accent">{leverage}x</span>
                  </div>
                  <input
                    type="range"
                    min={1}
                    max={125}
                    step={1}
                    value={leverage}
                    onChange={(e) => setLeverage(Number(e.target.value))}
                    className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-nexa-700 accent-accent"
                  />
                  <div className="mt-2 flex flex-wrap gap-1">
                    {LEVERAGE_PRESETS.map((lv) => (
                      <button
                        key={lv}
                        type="button"
                        onClick={() => setLeverage(lv)}
                        className={cls(
                          'rounded px-2 py-0.5 text-[11px] font-medium transition-colors',
                          leverage === lv ? 'bg-accent text-nexa-950' : 'bg-nexa-800 text-nexa-400 hover:bg-nexa-700'
                        )}
                      >
                        {lv}x
                      </button>
                    ))}
                  </div>
                </div>

                <div className="flex rounded-md border border-nexa-700/50 bg-nexa-900/40 p-0.5">
                  <button
                    type="button"
                    onClick={() => setOrderType('market')}
                    className={cls(
                      'flex-1 rounded px-3 py-1.5 text-xs font-medium transition-colors',
                      orderType === 'market' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                    )}
                  >
                    {t('futures.market')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setOrderType('limit')}
                    className={cls(
                      'flex-1 rounded px-3 py-1.5 text-xs font-medium transition-colors',
                      orderType === 'limit' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                    )}
                  >
                    {t('futures.limit')}
                  </button>
                </div>

                {orderType === 'limit' && (
                  <Input
                    label={`${t('futures.price')} (USDT)`}
                    type="number"
                    step="0.01"
                    placeholder="0.00"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    required
                    suffix="USDT"
                  />
                )}

                <div>
                  <div className="mb-1 flex items-center justify-between text-xs text-nexa-400">
                    <span>{t('futures.quantity')}</span>
                    <button
                      type="button"
                      onClick={fillMaxQty}
                      disabled={maxQty <= 0}
                      className="font-mono text-accent transition-opacity hover:opacity-80 disabled:opacity-30"
                    >
                      {t('futures.max')}: {maxQty > 0 ? maxQty.toFixed(4) : '—'}
                    </button>
                  </div>
                  <Input
                    type="number"
                    step="0.0001"
                    placeholder="0.00"
                    value={quantity}
                    onChange={(e) => setQuantity(e.target.value)}
                    required
                  />
                </div>

                <div className="grid grid-cols-2 gap-2">
                  <Input
                    label={t('futures.tpLabel')}
                    type="number"
                    step="0.01"
                    placeholder={t('futures.takeProfit')}
                    value={tpPrice}
                    onChange={(e) => setTpPrice(e.target.value)}
                  />
                  <Input
                    label={t('futures.slLabel')}
                    type="number"
                    step="0.01"
                    placeholder={t('futures.stopLoss')}
                    value={slPrice}
                    onChange={(e) => setSlPrice(e.target.value)}
                  />
                </div>

                <div className="space-y-1.5 rounded-md border border-nexa-700/50 bg-nexa-900/50 p-2.5 text-xs text-nexa-300">
                  <div className="flex justify-between">
                    <span>{t('futures.estMargin')}</span>
                    <span className="font-mono text-nexa-100">{estimatedMargin > 0 ? formatPrice(estimatedMargin, 2) : '—'} USDT</span>
                  </div>
                  <div className="flex justify-between">
                    <span>{t('futures.estLiqPrice')}</span>
                    <span className="font-mono text-nexa-100">{estimatedLiq > 0 ? formatPrice(estimatedLiq, 2) : '—'}</span>
                  </div>
                </div>

                <Button
                  type="submit"
                  variant={side === 'long' ? 'success' : 'danger'}
                  block
                  isLoading={submitting}
                >
                  {side === 'long' ? t('futures.openLong') : t('futures.openShort')}
                </Button>
              </form>
            </Card>

            <Card className="flex min-h-[280px] flex-col" title={t('futures.positionsOrders')}>
              <div className="flex border-b border-nexa-700/70 bg-nexa-900/40">
                <button
                  className={cls(
                    'flex-1 px-3 py-2 text-xs font-semibold transition-colors',
                    panelTab === 'positions'
                      ? 'border-b-2 border-accent text-nexa-100'
                      : 'text-nexa-400 hover:text-nexa-200'
                  )}
                  onClick={() => setPanelTab('positions')}
                >
                  {t('futures.positions')} <Badge color="neutral" size="sm" className="ml-1">{openPositions.length}</Badge>
                </button>
                <button
                  className={cls(
                    'flex-1 px-3 py-2 text-xs font-semibold transition-colors',
                    panelTab === 'orders'
                      ? 'border-b-2 border-accent text-nexa-100'
                      : 'text-nexa-400 hover:text-nexa-200'
                  )}
                  onClick={() => setPanelTab('orders')}
                >
                  {t('futures.orders')} <Badge color="neutral" size="sm" className="ml-1">{orders.length}</Badge>
                </button>
              </div>

              <div className="max-h-[520px] min-h-[200px] overflow-auto p-2">
                {panelTab === 'positions' && (
                  <div className="space-y-2">
                    <div className="flex border-b border-nexa-800/50">
                      <button
                        className={cls(
                          'flex-1 rounded-t px-2 py-1.5 text-xs font-medium transition-colors',
                          positionTab === 'open' ? 'text-nexa-100' : 'text-nexa-500 hover:text-nexa-300'
                        )}
                        onClick={() => setPositionTab('open')}
                      >
                        {t('futures.open')} ({openPositions.length})
                      </button>
                      <button
                        className={cls(
                          'flex-1 rounded-t px-2 py-1.5 text-xs font-medium transition-colors',
                          positionTab === 'closed' ? 'text-nexa-100' : 'text-nexa-500 hover:text-nexa-300'
                        )}
                        onClick={() => setPositionTab('closed')}
                      >
                        {t('futures.closed')} ({closedPositions.length})
                      </button>
                    </div>

                    {positionTab === 'open' && (
                      <div className="space-y-2">
                        {openPositions.map((p) => (
                          <div key={p.id} className="rounded-lg border border-nexa-700/60 bg-nexa-900/60 p-3 text-xs transition-colors hover:border-nexa-600">
                            <div className="mb-2 flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <span className="font-semibold text-nexa-100">{p.pair}</span>
                                <Badge color={p.side === 'long' ? 'up' : 'down'} size="sm">{p.side.toUpperCase()}</Badge>
                                <span className="rounded bg-nexa-800 px-1.5 py-0.5 text-[10px] text-nexa-300">{p.leverage}x</span>
                              </div>
                              <div className="flex items-center gap-1">
                                <Button size="sm" variant="ghost" onClick={() => toggleMarginEdit(p.id)} title={t('futures.margin')}>
                                  <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
                                    <path d="M3 7h18M3 12h12M3 17h6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                                  </svg>
                                </Button>
                                <Button size="sm" variant="ghost" onClick={() => toggleCloseEdit(p.id)} title={t('futures.partialClose')}>
                                  <svg viewBox="0 0 24 24" fill="none" className="h-3.5 w-3.5">
                                    <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                                  </svg>
                                </Button>
                                <Button size="sm" variant="danger" onClick={() => handleClose(p.id)} isLoading={closingId === p.id} disabled={!!closingId}>
                                  {t('futures.close')}
                                </Button>
                              </div>
                            </div>
                            <div className="grid grid-cols-2 gap-y-1 text-nexa-400">
                              <div>{t('futures.size')}: <span className="font-mono text-nexa-100">{formatQty(p.quantity, 6)}</span></div>
                              <div>{t('futures.entry')}: <span className="font-mono text-nexa-100">{formatPrice(p.entry_price, 2)}</span></div>
                              <div>{t('futures.mark')}: <span className="font-mono text-nexa-100">{formatPrice(p.mark_price, 2)}</span></div>
                              <div>{t('futures.liq')}: <span className="font-mono text-nexa-100">{formatPrice(p.liq_price, 2)}</span></div>
                              <div>{t('futures.tpLabel')}: <span className="font-mono text-nexa-100">{formatPrice(priceOrDash(p.tp_price), 2)}</span></div>
                              <div>{t('futures.slLabel')}: <span className="font-mono text-nexa-100">{formatPrice(priceOrDash(p.sl_price), 2)}</span></div>
                              <div className="col-span-2 mt-1 flex items-center justify-between border-t border-nexa-800/50 pt-1.5">
                                <span className="text-nexa-400">{t('futures.pnl')}:</span>
                                <span className={cls('font-mono font-semibold', changeColorClass(p.pnl))}>
                                  {formatPrice(p.pnl, 2)} <span className="opacity-70">({Number(p.pnl_pct || 0) >= 0 ? '+' : ''}{formatPct(p.pnl_pct)})</span>
                                </span>
                              </div>
                            </div>
                            {marginEditId === p.id && (
                              <div className="mt-2 flex items-center gap-1 border-t border-nexa-800/50 pt-2">
                                <Input
                                  type="number"
                                  step="0.01"
                                  placeholder={t('futures.amount')}
                                  value={marginAmount}
                                  onChange={(e) => setMarginAmount(e.target.value)}
                                  className="px-2 py-1 text-xs"
                                />
                                <Button size="sm" variant="success" onClick={() => handleAddMargin(p.id)} isLoading={marginLoading} className="px-2 py-1 text-xs">
                                  {t('futures.add')}
                                </Button>
                                <Button size="sm" variant="danger" onClick={() => handleReduceMargin(p.id)} isLoading={marginLoading} className="px-2 py-1 text-xs">
                                  {t('futures.reduce')}
                                </Button>
                              </div>
                            )}
                            {closeTargetId === p.id && (
                              <div className="mt-2 flex items-center gap-1 border-t border-nexa-800/50 pt-2">
                                <Input
                                  type="number"
                                  step="0.0001"
                                  placeholder={t('futures.closeQty')}
                                  value={closeQty}
                                  onChange={(e) => setCloseQty(e.target.value)}
                                  className="px-2 py-1 text-xs"
                                />
                                <Button size="sm" variant="secondary" onClick={() => handlePartialClose(p.id)} isLoading={closing} className="px-2 py-1 text-xs">
                                  {t('futures.close')}
                                </Button>
                              </div>
                            )}
                          </div>
                        ))}
                        {openPositions.length === 0 && (
                          <EmptyState title={t('futures.noOpenPositions')} compact />
                        )}
                      </div>
                    )}

                    {positionTab === 'closed' && (
                      <div className="space-y-2">
                        {closedPositions.map((p) => (
                          <div key={p.id} className="rounded-lg border border-nexa-800/60 bg-nexa-900/40 p-3 text-xs">
                            <div className="mb-2 flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <span className="font-semibold text-nexa-100">{p.pair}</span>
                                <Badge color={p.side === 'long' ? 'up' : 'down'} size="sm">{p.side.toUpperCase()}</Badge>
                                <span className="rounded bg-nexa-800 px-1.5 py-0.5 text-[10px] text-nexa-300">{p.leverage}x</span>
                              </div>
                            </div>
                            <div className="grid grid-cols-2 gap-y-1 text-nexa-400">
                              <div>{t('futures.size')}: <span className="font-mono text-nexa-100">{formatQty(p.quantity, 6)}</span></div>
                              <div>{t('futures.entry')}: <span className="font-mono text-nexa-100">{formatPrice(p.entry_price, 2)}</span></div>
                              <div className="col-span-2 mt-1 flex items-center justify-between border-t border-nexa-800/50 pt-1.5">
                                <span className="text-nexa-400">{t('futures.pnl')}:</span>
                                <span className={cls('font-mono font-semibold', changeColorClass(p.pnl))}>
                                  {formatPrice(p.pnl, 2)}
                                </span>
                              </div>
                            </div>
                          </div>
                        ))}
                        {closedPositions.length === 0 && (
                          <EmptyState title={t('futures.noClosedPositions')} compact />
                        )}
                      </div>
                    )}
                  </div>
                )}

                {panelTab === 'orders' && (
                  <div className="space-y-2">
                    {orders.map((o) => (
                      <div key={o.id} className="rounded-lg border border-nexa-800/60 bg-nexa-900/40 p-3 text-xs transition-colors hover:border-nexa-700">
                        <div className="mb-2 flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-nexa-100">{o.pair}</span>
                            <Badge color={o.side === 'buy' ? 'up' : 'down'} size="sm">{o.side.toUpperCase()}</Badge>
                            <span className="rounded bg-nexa-800 px-1.5 py-0.5 text-[10px] text-nexa-300">{o.leverage}x</span>
                          </div>
                          <Button size="sm" variant="secondary" onClick={() => handleCancelOrder(o.id)}>
                            {t('futures.cancel')}
                          </Button>
                        </div>
                        <div className="grid grid-cols-2 gap-y-1 text-nexa-400">
                          <div>{t('futures.type')}: <span className="font-mono text-nexa-100">{o.type.toUpperCase()}</span></div>
                          <div>{t('futures.price')}: <span className="font-mono text-nexa-100">{o.price ? formatPrice(o.price, 2) : t('futures.market')}</span></div>
                          <div>{t('futures.size')}: <span className="font-mono text-nexa-100">{formatQty(o.quantity, 6)}</span></div>
                          <div>{t('futures.tpLabel')}: <span className="font-mono text-nexa-100">{formatPrice(priceOrDash(o.tp_price), 2)}</span></div>
                          <div>{t('futures.slLabel')}: <span className="font-mono text-nexa-100">{formatPrice(priceOrDash(o.sl_price), 2)}</span></div>
                          <div className="col-span-2">
                            {t('futures.created')}: <span className="font-mono text-nexa-100">{formatTime(o.created_at)}</span>
                          </div>
                        </div>
                      </div>
                    ))}
                    {orders.length === 0 && (
                      <EmptyState title={t('futures.noOpenOrders')} compact />
                    )}
                  </div>
                )}
              </div>
            </Card>
          </div>
        </div>
      </div>
    </Layout>
  );
}
