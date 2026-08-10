import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Select } from '@/components/common/Select';
import { Tabs } from '@/components/common/Tabs';
import { Badge } from '@/components/common/Badge';
import { EmptyState } from '@/components/common/EmptyState';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { useAuthStore } from '@/store/authStore';
import { formatQty, formatPrice, formatTime, cls } from '@/utils/format';
import { toast } from '@/store/toastStore';
import {
  getAmmPools,
  getAmmPool,
  createAmmPool,
  addLiquidity,
  removeLiquidity,
  getAmmPosition,
  getAmmPositions,
  getAmmSwaps,
  quoteAmmSwap,
  executeAmmSwap,
  type AmmPool,
  type AmmPosition,
} from '@/api/amm';

function poolPrice(pool: AmmPool) {
  const r0 = parseFloat(pool.reserve0 || '0');
  const r1 = parseFloat(pool.reserve1 || '0');
  if (r0 <= 0) return 0;
  return r1 / r0;
}

/** Common slippage tolerance presets, in percent. */
const SLIPPAGE_PRESETS = [0.5, 1, 3];

/**
 * Computes min_amount_out = quote output × (1 − slippage) as a big-number
 * string. Returns '' when protection is disabled or the inputs are invalid.
 */
function computeMinAmountOut(amountOut: string | undefined, slippagePct: number): string {
  const out = parseFloat(amountOut || '0');
  if (!isFinite(out) || out <= 0 || !isFinite(slippagePct) || slippagePct <= 0 || slippagePct >= 100) {
    return '';
  }
  const min = out * (1 - slippagePct / 100);
  if (!isFinite(min) || min <= 0) return '';
  return min.toFixed(18);
}

/** Extracts a human-readable message from an axios/backend error. */
function apiErrorMessage(err: unknown): string {
  const e = err as {
    response?: { data?: { error?: string; message?: string } };
    message?: string;
  };
  return e?.response?.data?.error || e?.response?.data?.message || e?.message || '';
}

/** True when the backend rejected the swap due to slippage protection. */
function isSlippageError(err: unknown): boolean {
  return apiErrorMessage(err).toLowerCase().includes('slippage');
}

export function AMMPage() {
  const { t } = useTranslation();
  const { isAdmin } = useAuthStore();
  const { data: pools, refetch: refetchPools } = useFetch(getAmmPools, []);
  const { data: positions, refetch: refetchPositions } = useFetch(getAmmPositions, []);

  const [selectedPoolId, setSelectedPoolId] = useState('');
  const [tab, setTab] = useState<'swap' | 'liquidity' | 'create'>('swap');

  const selectedPool = useMemo(
    () => pools?.find((p) => p.id === selectedPoolId) || pools?.[0] || null,
    [pools, selectedPoolId]
  );

  useEffect(() => {
    if (pools?.length && !selectedPoolId) {
      setSelectedPoolId(pools[0].id);
    }
  }, [pools, selectedPoolId]);

  const { data: poolDetail, refetch: refetchPoolDetail } = useFetch(
    () => (selectedPoolId ? getAmmPool(selectedPoolId) : Promise.resolve(null)),
    [selectedPoolId]
  );
  const { data: position, refetch: refetchPosition } = useFetch(
    () => (selectedPoolId ? getAmmPosition(selectedPoolId) : Promise.resolve(null)),
    [selectedPoolId]
  );
  const { data: swaps, refetch: refetchSwaps } = useFetch(
    () => (selectedPoolId ? getAmmSwaps(selectedPoolId, 20) : Promise.resolve([])),
    [selectedPoolId]
  );

  usePolling(() => {
    refetchPools();
    refetchPoolDetail();
    refetchPosition();
    refetchSwaps();
  }, 4000);

  const pool = poolDetail || selectedPool;

  const handleTabChange = useCallback((id: string) => {
    setTab(id as 'swap' | 'liquidity' | 'create');
  }, []);

  const refreshAll = useCallback(() => {
    refetchPools();
    refetchPoolDetail();
    refetchPosition();
    refetchPositions();
    refetchSwaps();
  }, [refetchPools, refetchPoolDetail, refetchPosition, refetchPositions, refetchSwaps]);

  const tabItems = useMemo(
    () => {
      const items = [
        {
          id: 'swap',
          label: t('amm.swap'),
          content: <SwapPanel pool={pool} onUpdate={refreshAll} />,
        },
        {
          id: 'liquidity',
          label: t('amm.liquidity'),
          content: (
            <LiquidityPanel
              pool={pool}
              position={position}
              onUpdate={refreshAll}
            />
          ),
        },
      ];
      // Only admins can create pools (POST /amm/pools is admin-guarded on the
      // backend). Hiding the tab avoids surfacing a form a regular user could
      // never submit successfully.
      if (isAdmin) {
        items.push({
          id: 'create',
          label: t('amm.createPool'),
          content: <CreatePoolPanel onUpdate={refreshAll} />,
        });
      }
      return items;
    },
    [pool, position, refreshAll, isAdmin, t]
  );

  // If the user lost admin rights (e.g. logged out + back in as a regular user)
  // while on the create tab, snap back to swap.
  useEffect(() => {
    if (tab === 'create' && !isAdmin) setTab('swap');
  }, [tab, isAdmin]);

  return (
    <Layout>
      <div className="mx-auto grid max-w-7xl grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-1">
          <Card title={t('amm.pools')}>
            <div className="max-h-[28rem] overflow-y-auto p-2">
              {(pools || []).length === 0 && (
                <EmptyState title={t('amm.noPools')} compact />
              )}
              {(pools || []).map((p) => {
                const pPrice = poolPrice(p);
                return (
                  <button
                    key={p.id}
                    onClick={() => setSelectedPoolId(p.id)}
                    className={cls(
                      'mb-1.5 w-full rounded-lg border px-3 py-2 text-left text-sm transition-all',
                      pool?.id === p.id
                        ? 'border-accent/50 bg-accent/10 shadow-sm'
                        : 'border-nexa-800 bg-nexa-900/30 hover:border-nexa-600 hover:bg-nexa-800/40'
                    )}
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-semibold text-nexa-100">{p.pair}</span>
                      <span className="font-mono text-xs text-nexa-300">{formatPrice(pPrice)}</span>
                    </div>
                    <div className="mt-1 flex items-center justify-between text-xs text-nexa-500">
                      <span>R0 {formatQty(p.reserve0, 4)} · R1 {formatQty(p.reserve1, 4)}</span>
                      <Badge color="neutral" size="sm">{(parseFloat(p.fee_rate || '0') * 100).toFixed(2)}%</Badge>
                    </div>
                  </button>
                );
              })}
            </div>
          </Card>

          <Card title={t('amm.myLiquidity')}>
            <div className="max-h-64 space-y-2 overflow-y-auto p-2">
              {(positions || []).length === 0 && (
                <EmptyState title={t('amm.noPositions')} compact />
              )}
              {(positions || []).map((pos) => {
                const p = pools?.find((x) => x.id === pos.pool_id);
                if (!p) return null;
                const shareRatio = parseFloat(p.lp_shares || '0') > 0
                  ? parseFloat(pos.shares) / parseFloat(p.lp_shares)
                  : 0;
                return (
                  <button
                    key={pos.id}
                    onClick={() => setSelectedPoolId(p.id)}
                    className="block w-full rounded-lg border border-nexa-800 bg-nexa-900/30 p-3 text-left text-sm transition-all hover:border-nexa-600 hover:bg-nexa-800/40"
                  >
                    <div className="flex items-center justify-between">
                      <div className="font-medium text-nexa-100">{p.pair}</div>
                      <Badge color="accent" size="sm">{formatQty(pos.shares, 4)} shares</Badge>
                    </div>
                    <div className="mt-1 text-xs text-nexa-500">
                      ~{formatQty((parseFloat(p.reserve0 || '0') * shareRatio).toString(), 4)} {p.token0}
                      {' / '}
                      {formatQty((parseFloat(p.reserve1 || '0') * shareRatio).toString(), 4)} {p.token1}
                    </div>
                  </button>
                );
              })}
            </div>
          </Card>
        </div>

        <div className="space-y-4 lg:col-span-2">
          {pool && (
            <Card title={t('amm.poolCard', { pair: pool.pair })} extra={
              <Badge color="accent" size="sm">
                Fee {(parseFloat(pool.fee_rate || '0') * 100).toFixed(2)}%
              </Badge>
            }>
              <div className="grid grid-cols-2 gap-4 p-4 text-sm md:grid-cols-4">
                <div>
                  <div className="text-xs uppercase tracking-wide text-nexa-500">{t('amm.price')}</div>
                  <div className="mt-1 font-mono text-base text-nexa-100">{formatPrice(poolPrice(pool))}</div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-wide text-nexa-500">{pool.token0} {t('amm.reserve')}</div>
                  <div className="mt-1 font-mono text-nexa-100">{formatQty(pool.reserve0, 6)}</div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-wide text-nexa-500">{pool.token1} {t('amm.reserve')}</div>
                  <div className="mt-1 font-mono text-nexa-100">{formatQty(pool.reserve1, 6)}</div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-wide text-nexa-500">{t('amm.lpShares')}</div>
                  <div className="mt-1 font-mono text-nexa-100">{formatQty(pool.lp_shares, 6)}</div>
                </div>
              </div>
              {position && (
                <div className="border-t border-nexa-800/50 px-4 py-3 text-sm">
                  <span className="text-nexa-400">{t('amm.yourPositionLabel')}</span>{' '}
                  <span className="font-mono font-semibold text-nexa-100">{formatQty(position.shares, 6)}</span> {t('amm.shares')}
                </div>
              )}
            </Card>
          )}

          <Tabs
            activeTab={tab}
            onChange={handleTabChange}
            tabs={tabItems}
          />

          {pool && (
            <Card title={t('amm.recentSwaps')} extra={
              <span className="flex items-center gap-1.5 text-up">
                <span className="h-1.5 w-1.5 rounded-full bg-up animate-pulse-soft" />Live
              </span>
            }>
              <div className="maxh-64 max-h-64 overflow-y-auto p-2">
                <table className="w-full text-left text-sm">
                  <thead className="text-nexa-400">
                    <tr>
                      <th className="px-3 py-2">{t('trading.time')}</th>
                      <th className="px-3 py-2">{t('amm.in')}</th>
                      <th className="px-3 py-2">{t('amm.out')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(swaps || []).length === 0 && (
                      <tr>
                        <td colSpan={3}>
                          <EmptyState title={t('amm.noSwaps')} compact />
                        </td>
                      </tr>
                    )}
                    {(swaps || []).map((s) => (
                      <tr key={s.id} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
                        <td className="px-3 py-2 text-nexa-400">
                          {formatTime(s.created_at)}
                        </td>
                        <td className="px-3 py-2 font-mono text-down">
                          −{formatQty(s.amount_in, 4)} {s.token_in}
                        </td>
                        <td className="px-3 py-2 font-mono text-up">
                          +{formatQty(s.amount_out, 4)} {s.token_out}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
        </div>
      </div>
    </Layout>
  );
}

function SwapPanel({
  pool,
  onUpdate,
}: {
  pool: AmmPool | null;
  onUpdate: () => void;
}) {
  const { t } = useTranslation();
  const [amountIn, setAmountIn] = useState('');
  const [tokenIn, setTokenIn] = useState('');
  const [quoting, setQuoting] = useState(false);
  const [quote, setQuote] = useState<{ amount_out: string; fee: string } | null>(null);
  const [swapping, setSwapping] = useState(false);
  // Slippage protection: selected tolerance in percent. `customMode` switches
  // the preset buttons for a free-form input.
  const [slippagePreset, setSlippagePreset] = useState<number>(1);
  const [customMode, setCustomMode] = useState(false);
  const [customSlippage, setCustomSlippage] = useState('');

  const slippage = customMode ? parseFloat(customSlippage) : slippagePreset;
  const slippageValid = isFinite(slippage) && slippage >= 0 && slippage < 100;
  const minAmountOut = useMemo(
    () => (slippageValid ? computeMinAmountOut(quote?.amount_out, slippage) : ''),
    [quote?.amount_out, slippage, slippageValid]
  );

  useEffect(() => {
    if (pool) setTokenIn(pool.token0);
  }, [pool?.token0]);

  useEffect(() => {
    setQuote(null);
    if (!pool || !amountIn || parseFloat(amountIn) <= 0 || !tokenIn) return;
    let cancelled = false;
    const run = async () => {
      setQuoting(true);
      try {
        const q = await quoteAmmSwap({ pool_id: pool.id, token_in: tokenIn, amount_in: amountIn });
        if (!cancelled) setQuote(q);
      } catch {
        if (!cancelled) setQuote(null);
      } finally {
        if (!cancelled) setQuoting(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [pool, amountIn, tokenIn]);

  if (!pool) return <EmptyState title={t('amm.noPools')} compact />;

  const tokenOut = tokenIn === pool.token0 ? pool.token1 : pool.token0;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!amountIn || parseFloat(amountIn) <= 0) return;
    if (swapping) return;
    setSwapping(true);
    try {
      const out = await executeAmmSwap({
        pool_id: pool.id,
        token_in: tokenIn,
        amount_in: amountIn,
        min_amount_out: minAmountOut,
      });
      toast.success(t('amm.swapped', {
        amountIn,
        tokenIn,
        amountOut: out?.amount_out || quote?.amount_out || '0',
        tokenOut,
      }));
      setAmountIn('');
      setQuote(null);
      onUpdate();
    } catch (err: unknown) {
      if (isSlippageError(err)) {
        toast.error(t('amm.slippageExceeded'));
      } else {
        toast.error(apiErrorMessage(err) || t('amm.swap'));
      }
    } finally {
      setSwapping(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 p-2">
      <Select label={t('amm.tokenIn')} value={tokenIn} onChange={(e) => setTokenIn(e.target.value)}>
        <option value={pool.token0}>{pool.token0}</option>
        <option value={pool.token1}>{pool.token1}</option>
      </Select>
      <Input
        label={`${t('amm.amountIn')} (${tokenIn})`}
        type="number"
        step="any"
        value={amountIn}
        onChange={(e) => setAmountIn(e.target.value)}
        required
        suffix={tokenIn}
      />
      {quote && (
        <div className="space-y-2 rounded-lg border border-nexa-700/70 bg-nexa-900/50 p-3 text-sm">
          <div className="flex justify-between">
            <span className="text-nexa-400">{t('amm.youReceive')}</span>
            <span className="font-mono font-semibold text-up">
              {formatQty(quote.amount_out, 6)} {tokenOut}
            </span>
          </div>
          <div className="flex justify-between text-xs">
            <span className="text-nexa-400">{t('amm.fee')}</span>
            <span className="font-mono text-nexa-300">{formatQty(quote.fee, 6)} {tokenIn}</span>
          </div>
          <div className="flex justify-between text-xs">
            <span className="text-nexa-400">{t('amm.minReceive')}</span>
            <span className="font-mono text-nexa-300">
              {minAmountOut ? `${formatQty(minAmountOut, 6)} ${tokenOut}` : '—'}
            </span>
          </div>
        </div>
      )}
      <div className="space-y-2">
        <div className="flex items-center justify-between text-xs">
          <span className="text-nexa-400">{t('amm.slippageTolerance')}</span>
          <span className={cls('font-mono', slippageValid ? 'text-nexa-200' : 'text-down')}>
            {slippageValid ? `${slippage}%` : t('amm.slippageInvalid')}
          </span>
        </div>
        <div className="flex gap-1.5">
          {SLIPPAGE_PRESETS.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => { setCustomMode(false); setSlippagePreset(p); }}
              className={cls(
                'flex-1 rounded-md border px-2 py-1.5 text-xs font-medium transition-all',
                !customMode && slippagePreset === p
                  ? 'border-accent/60 bg-accent/15 text-accent'
                  : 'border-nexa-700 bg-nexa-900/40 text-nexa-300 hover:border-nexa-500'
              )}
            >
              {p}%
            </button>
          ))}
          <button
            type="button"
            onClick={() => setCustomMode(true)}
            className={cls(
              'flex-1 rounded-md border px-2 py-1.5 text-xs font-medium transition-all',
              customMode
                ? 'border-accent/60 bg-accent/15 text-accent'
                : 'border-nexa-700 bg-nexa-900/40 text-nexa-300 hover:border-nexa-500'
            )}
          >
            {t('amm.slippageCustom')}
          </button>
        </div>
        {customMode && (
          <Input
            type="number"
            step="any"
            min="0"
            max="99"
            value={customSlippage}
            onChange={(e) => setCustomSlippage(e.target.value)}
            placeholder="0.5"
            suffix="%"
          />
        )}
      </div>
      {quoting && (
        <div className="flex items-center gap-2 text-xs text-nexa-500">
          <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-accent border-t-transparent" />
          {t('amm.updatingQuote')}
        </div>
      )}
      <Button type="submit" isLoading={swapping} block>
        {t('amm.swap')} {tokenIn} → {tokenOut}
      </Button>
    </form>
  );
}

function LiquidityPanel({
  pool,
  position,
  onUpdate,
}: {
  pool: AmmPool | null;
  position: AmmPosition | null;
  onUpdate: () => void;
}) {
  const { t } = useTranslation();
  const [amount0, setAmount0] = useState('');
  const [amount1, setAmount1] = useState('');
  const [adding, setAdding] = useState(false);
  const [removeShares, setRemoveShares] = useState('');
  const [removing, setRemoving] = useState(false);

  if (!pool) return <EmptyState title={t('amm.noPools')} compact />;

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!amount0 || !amount1 || parseFloat(amount0) <= 0 || parseFloat(amount1) <= 0) return;
    if (adding) return;
    setAdding(true);
    try {
      const res = await addLiquidity(pool.id, { amount0, amount1 });
      toast.success(
        `${t('amm.addLiquidity')}: ${formatQty(res.amount0, 4)} ${pool.token0} + ${formatQty(res.amount1, 4)} ${pool.token1} → ${formatQty(res.shares_minted, 6)}`
      );
      setAmount0('');
      setAmount1('');
      onUpdate();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('amm.addLiquidity'));
    } finally {
      setAdding(false);
    }
  };

  const remove = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!removeShares || parseFloat(removeShares) <= 0) return;
    if (removing) return;
    setRemoving(true);
    try {
      const res = await removeLiquidity(pool.id, { shares: removeShares });
      toast.success(
        `${t('amm.removeLiquidity')}: ${formatQty(res.shares, 6)} → ${formatQty(res.amount0, 4)} ${pool.token0} + ${formatQty(res.amount1, 4)} ${pool.token1}`
      );
      setRemoveShares('');
      onUpdate();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('amm.removeLiquidity'));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <div className="grid grid-cols-1 gap-4 p-2 md:grid-cols-2">
      <form onSubmit={add} className="space-y-3 rounded-lg border border-nexa-700/50 bg-nexa-900/30 p-3">
        <h3 className="text-sm font-semibold text-up">{t('amm.addLiquidity')}</h3>
        <Input
          label={`${pool.token0} ${t('wallet.amount')}`}
          type="number"
          step="any"
          value={amount0}
          onChange={(e) => setAmount0(e.target.value)}
          required
          suffix={pool.token0}
        />
        <Input
          label={`${pool.token1} ${t('wallet.amount')}`}
          type="number"
          step="any"
          value={amount1}
          onChange={(e) => setAmount1(e.target.value)}
          required
          suffix={pool.token1}
        />
        <Button type="submit" variant="success" isLoading={adding} block>
          {t('amm.addLiquidity')}
        </Button>
      </form>

      <form onSubmit={remove} className="space-y-3 rounded-lg border border-nexa-700/50 bg-nexa-900/30 p-3">
        <h3 className="text-sm font-semibold text-down">{t('amm.removeLiquidity')}</h3>
        <Input
          label={t('amm.sharesToBurn')}
          type="number"
          step="any"
          value={removeShares}
          onChange={(e) => setRemoveShares(e.target.value)}
          required
          suffix="LP"
        />
        {position && (
          <div className="text-xs text-nexa-400">
            {t('amm.max')}: <span className="font-mono text-nexa-200">{formatQty(position.shares, 6)}</span>
          </div>
        )}
        <Button type="submit" variant="danger" isLoading={removing} block>
          {t('amm.removeLiquidity')}
        </Button>
      </form>
    </div>
  );
}

function CreatePoolPanel({
  onUpdate,
}: {
  onUpdate: () => void;
}) {
  const { t } = useTranslation();
  const [pair, setPair] = useState('');
  const [token0, setToken0] = useState('');
  const [token1, setToken1] = useState('');
  const [fee, setFee] = useState('0.003');
  const [creating, setCreating] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pair || !token0 || !token1) return;
    if (creating) return;
    setCreating(true);
    try {
      await createAmmPool({ pair, token0, token1, fee_rate: parseFloat(fee) });
      toast.success(t('amm.poolCreated', { pair }));
      setPair('');
      setToken0('');
      setToken1('');
      onUpdate();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('amm.createPool'));
    } finally {
      setCreating(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 p-2 md:max-w-md">
      <Input label={`${t('amm.pair')} (e.g. ETH/USDT)`} value={pair} onChange={(e) => setPair(e.target.value)} required placeholder="ETH/USDT" />
      <Input label={t('amm.token0')} value={token0} onChange={(e) => setToken0(e.target.value)} required placeholder="ETH" />
      <Input label={t('amm.token1')} value={token1} onChange={(e) => setToken1(e.target.value)} required placeholder="USDT" />
      <Input label={t('amm.feeRate')} type="number" step="any" value={fee} onChange={(e) => setFee(e.target.value)} required hint="0.003 = 0.3%" />
      <Button type="submit" isLoading={creating} block>
        {t('amm.createPool')}
      </Button>
    </form>
  );
}
