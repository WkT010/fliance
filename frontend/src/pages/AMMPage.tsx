import { useState, useEffect, useMemo, useCallback } from 'react';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Select } from '@/components/common/Select';
import { Tabs } from '@/components/common/Tabs';
import { useFetch } from '@/hooks/useFetch';
import { usePolling } from '@/hooks/usePolling';
import { formatQty, formatPrice, cls } from '@/utils/format';
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

export function AMMPage() {
  const { data: pools, refetch: refetchPools } = useFetch(getAmmPools, []);
  const { data: positions, refetch: refetchPositions } = useFetch(getAmmPositions, []);

  const [selectedPoolId, setSelectedPoolId] = useState('');
  const [tab, setTab] = useState<'swap' | 'liquidity' | 'create'>('swap');
  const [msg, setMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

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
    () => [
      {
        id: 'swap',
        label: 'Swap',
        content: <SwapPanel pool={pool} onUpdate={refreshAll} setMsg={setMsg} />,
      },
      {
        id: 'liquidity',
        label: 'Liquidity',
        content: (
          <LiquidityPanel
            pool={pool}
            position={position}
            onUpdate={refreshAll}
            setMsg={setMsg}
          />
        ),
      },
      {
        id: 'create',
        label: 'Create Pool',
        content: <CreatePoolPanel onUpdate={refreshAll} setMsg={setMsg} />,
      },
    ],
    [pool, position, refreshAll, setMsg]
  );

  return (
    <Layout>
      <div className="mx-auto grid max-w-7xl grid-cols-1 gap-4 p-4 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-1">
          <Card title="AMM Pools">
            <div className="max-h-[28rem] overflow-y-auto p-2">
              {(pools || []).length === 0 && (
                <div className="p-4 text-center text-sm text-nexa-500">No pools yet</div>
              )}
              {(pools || []).map((p) => (
                <button
                  key={p.id}
                  onClick={() => setSelectedPoolId(p.id)}
                  className={cls(
                    'w-full rounded border px-3 py-2 text-left text-sm transition-colors',
                    pool?.id === p.id
                      ? 'border-accent bg-accent/10'
                      : 'border-nexa-800 bg-nexa-900/30 hover:bg-nexa-800/40'
                  )}
                >
                  <div className="flex items-center justify-between">
                    <span className="font-medium text-nexa-100">{p.pair}</span>
                    <span className="text-xs text-nexa-400">{formatPrice(poolPrice(p))}</span>
                  </div>
                  <div className="mt-1 text-xs text-nexa-500">
                    R0 {formatQty(p.reserve0, 4)} · R1 {formatQty(p.reserve1, 4)} · Fee {(parseFloat(p.fee_rate || '0') * 100).toFixed(2)}%
                  </div>
                </button>
              ))}
            </div>
          </Card>

          <Card title="My Liquidity">
            <div className="max-h-64 space-y-2 overflow-y-auto p-2">
              {(positions || []).length === 0 && (
                <div className="p-2 text-center text-sm text-nexa-500">No LP positions</div>
              )}
              {(positions || []).map((pos) => {
                const p = pools?.find((x) => x.id === pos.pool_id);
                if (!p) return null;
                const shareRatio = parseFloat(p.lp_shares || '0') > 0
                  ? parseFloat(pos.shares) / parseFloat(p.lp_shares)
                  : 0;
                return (
                  <div key={pos.id} className="rounded border border-nexa-800 bg-nexa-900/30 p-3 text-sm">
                    <div className="font-medium text-nexa-100">{p.pair}</div>
                    <div className="text-xs text-nexa-400">Shares {formatQty(pos.shares, 6)}</div>
                    <div className="mt-1 text-xs text-nexa-500">
                      ~{formatQty((parseFloat(p.reserve0 || '0') * shareRatio).toString(), 4)} {p.token0}
                      {' / '}
                      {formatQty((parseFloat(p.reserve1 || '0') * shareRatio).toString(), 4)} {p.token1}
                    </div>
                  </div>
                );
              })}
            </div>
          </Card>
        </div>

        <div className="space-y-4 lg:col-span-2">
          {pool && (
            <Card title={`${pool.pair} Pool`}>
              <div className="grid grid-cols-2 gap-4 p-4 text-sm md:grid-cols-4">
                <div>
                  <div className="text-xs text-nexa-500">Price</div>
                  <div className="font-mono text-nexa-100">{formatPrice(poolPrice(pool))}</div>
                </div>
                <div>
                  <div className="text-xs text-nexa-500">{pool.token0} Reserve</div>
                  <div className="font-mono text-nexa-100">{formatQty(pool.reserve0, 6)}</div>
                </div>
                <div>
                  <div className="text-xs text-nexa-500">{pool.token1} Reserve</div>
                  <div className="font-mono text-nexa-100">{formatQty(pool.reserve1, 6)}</div>
                </div>
                <div>
                  <div className="text-xs text-nexa-500">LP Shares</div>
                  <div className="font-mono text-nexa-100">{formatQty(pool.lp_shares, 6)}</div>
                </div>
              </div>
              {position && (
                <div className="border-t border-nexa-800/50 px-4 py-3 text-sm">
                  <span className="text-nexa-400">Your position:</span>{' '}
                  <span className="font-mono text-nexa-100">{formatQty(position.shares, 6)}</span> shares
                </div>
              )}
            </Card>
          )}

          {msg && (
            <div
              className={cls(
                'rounded border px-3 py-2 text-sm',
                msg.type === 'success'
                  ? 'border-up/20 bg-up/10 text-up'
                  : 'border-down/20 bg-down/10 text-down'
              )}
            >
              {msg.text}
            </div>
          )}

          <Tabs
            activeTab={tab}
            onChange={handleTabChange}
            tabs={tabItems}
          />

          {pool && (
            <Card title="Recent Swaps">
              <div className="max-h-64 overflow-y-auto p-2">
                <table className="w-full text-left text-sm">
                  <thead className="text-nexa-400">
                    <tr>
                      <th className="px-3 py-2">Time</th>
                      <th className="px-3 py-2">In</th>
                      <th className="px-3 py-2">Out</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(swaps || []).length === 0 && (
                      <tr>
                        <td colSpan={3} className="px-3 py-4 text-center text-nexa-500">
                          No swaps
                        </td>
                      </tr>
                    )}
                    {(swaps || []).map((s) => (
                      <tr key={s.id} className="border-b border-nexa-800/50">
                        <td className="px-3 py-2 text-nexa-400">
                          {new Date(s.created_at / 1e6).toLocaleTimeString()}
                        </td>
                        <td className="px-3 py-2 font-mono">
                          {formatQty(s.amount_in, 4)} {s.token_in}
                        </td>
                        <td className="px-3 py-2 font-mono">
                          {formatQty(s.amount_out, 4)} {s.token_out}
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
  setMsg,
}: {
  pool: AmmPool | null;
  onUpdate: () => void;
  setMsg: (m: { text: string; type: 'success' | 'error' } | null) => void;
}) {
  const [amountIn, setAmountIn] = useState('');
  const [tokenIn, setTokenIn] = useState('');
  const [quoting, setQuoting] = useState(false);
  const [quote, setQuote] = useState<{ amount_out: string; fee: string } | null>(null);
  const [swapping, setSwapping] = useState(false);

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

  if (!pool) return <div className="p-4 text-sm text-nexa-500">Select or create a pool first.</div>;

  const tokenOut = tokenIn === pool.token0 ? pool.token1 : pool.token0;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!amountIn || parseFloat(amountIn) <= 0) return;
    setSwapping(true);
    setMsg(null);
    try {
      await executeAmmSwap({ pool_id: pool.id, token_in: tokenIn, amount_in: amountIn });
      setMsg({ text: `Swapped ${amountIn} ${tokenIn} → ${quote?.amount_out || ''} ${tokenOut}`, type: 'success' });
      setAmountIn('');
      setQuote(null);
      onUpdate();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Swap failed', type: 'error' });
    } finally {
      setSwapping(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 p-2">
      <Select label="Token In" value={tokenIn} onChange={(e) => setTokenIn(e.target.value)}>
        <option value={pool.token0}>{pool.token0}</option>
        <option value={pool.token1}>{pool.token1}</option>
      </Select>
      <Input
        label={`Amount In (${tokenIn})`}
        type="number"
        step="any"
        value={amountIn}
        onChange={(e) => setAmountIn(e.target.value)}
        required
      />
      {quote && (
        <div className="space-y-1 rounded border border-nexa-700 bg-nexa-900/50 p-3 text-sm">
          <div className="flex justify-between">
            <span className="text-nexa-400">You receive</span>
            <span className="font-mono text-nexa-100">
              {formatQty(quote.amount_out, 6)} {tokenOut}
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-nexa-400">Fee</span>
            <span className="font-mono text-nexa-100">{formatQty(quote.fee, 6)} {tokenIn}</span>
          </div>
        </div>
      )}
      {quoting && <div className="text-xs text-nexa-500">Updating quote...</div>}
      <Button type="submit" isLoading={swapping} className="w-full">
        Swap {tokenIn} → {tokenOut}
      </Button>
    </form>
  );
}

function LiquidityPanel({
  pool,
  position,
  onUpdate,
  setMsg,
}: {
  pool: AmmPool | null;
  position: AmmPosition | null;
  onUpdate: () => void;
  setMsg: (m: { text: string; type: 'success' | 'error' } | null) => void;
}) {
  const [amount0, setAmount0] = useState('');
  const [amount1, setAmount1] = useState('');
  const [adding, setAdding] = useState(false);
  const [removeShares, setRemoveShares] = useState('');
  const [removing, setRemoving] = useState(false);

  if (!pool) return <div className="p-4 text-sm text-nexa-500">Select or create a pool first.</div>;

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!amount0 || !amount1 || parseFloat(amount0) <= 0 || parseFloat(amount1) <= 0) return;
    setAdding(true);
    setMsg(null);
    try {
      const res = await addLiquidity(pool.id, { amount0, amount1 });
      setMsg({
        text: `Added liquidity: ${formatQty(res.amount0, 4)} ${pool.token0} + ${formatQty(res.amount1, 4)} ${pool.token1} → ${formatQty(res.shares_minted, 6)} shares`,
        type: 'success',
      });
      setAmount0('');
      setAmount1('');
      onUpdate();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Add liquidity failed', type: 'error' });
    } finally {
      setAdding(false);
    }
  };

  const remove = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!removeShares || parseFloat(removeShares) <= 0) return;
    setRemoving(true);
    setMsg(null);
    try {
      const res = await removeLiquidity(pool.id, { shares: removeShares });
      setMsg({
        text: `Removed liquidity: ${formatQty(res.shares, 6)} shares → ${formatQty(res.amount0, 4)} ${pool.token0} + ${formatQty(res.amount1, 4)} ${pool.token1}`,
        type: 'success',
      });
      setRemoveShares('');
      onUpdate();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Remove liquidity failed', type: 'error' });
    } finally {
      setRemoving(false);
    }
  };

  return (
    <div className="grid grid-cols-1 gap-4 p-2 md:grid-cols-2">
      <form onSubmit={add} className="space-y-3">
        <h3 className="text-sm font-medium text-nexa-100">Add Liquidity</h3>
        <Input
          label={`${pool.token0} Amount`}
          type="number"
          step="any"
          value={amount0}
          onChange={(e) => setAmount0(e.target.value)}
          required
        />
        <Input
          label={`${pool.token1} Amount`}
          type="number"
          step="any"
          value={amount1}
          onChange={(e) => setAmount1(e.target.value)}
          required
        />
        <Button type="submit" isLoading={adding} className="w-full">
          Add Liquidity
        </Button>
      </form>

      <form onSubmit={remove} className="space-y-3">
        <h3 className="text-sm font-medium text-nexa-100">Remove Liquidity</h3>
        <Input
          label="Shares to Burn"
          type="number"
          step="any"
          value={removeShares}
          onChange={(e) => setRemoveShares(e.target.value)}
          required
        />
        {position && (
          <div className="text-xs text-nexa-400">Max: {formatQty(position.shares, 6)} shares</div>
        )}
        <Button type="submit" variant="secondary" isLoading={removing} className="w-full">
          Remove Liquidity
        </Button>
      </form>
    </div>
  );
}

function CreatePoolPanel({
  onUpdate,
  setMsg,
}: {
  onUpdate: () => void;
  setMsg: (m: { text: string; type: 'success' | 'error' } | null) => void;
}) {
  const [pair, setPair] = useState('');
  const [token0, setToken0] = useState('');
  const [token1, setToken1] = useState('');
  const [fee, setFee] = useState('0.003');
  const [creating, setCreating] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pair || !token0 || !token1) return;
    setCreating(true);
    setMsg(null);
    try {
      await createAmmPool({ pair, token0, token1, fee_rate: parseFloat(fee) });
      setMsg({ text: `Pool ${pair} created`, type: 'success' });
      setPair('');
      setToken0('');
      setToken1('');
      onUpdate();
    } catch (err: unknown) {
      setMsg({ text: err instanceof Error ? err.message : 'Create pool failed', type: 'error' });
    } finally {
      setCreating(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 p-2 md:max-w-md">
      <Input label="Pair (e.g. ETH/USDT)" value={pair} onChange={(e) => setPair(e.target.value)} required />
      <Input label="Token 0" value={token0} onChange={(e) => setToken0(e.target.value)} required />
      <Input label="Token 1" value={token1} onChange={(e) => setToken1(e.target.value)} required />
      <Input label="Fee Rate" type="number" step="any" value={fee} onChange={(e) => setFee(e.target.value)} required />
      <Button type="submit" isLoading={creating} className="w-full">
        Create Pool
      </Button>
    </form>
  );
}
