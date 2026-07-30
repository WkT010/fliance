import { useState, useMemo, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
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
import { formatPrice, formatQty, cls, changeColorClass } from '@/utils/format';
import {
  getMarkPrice,
  getFuturesPositions,
  openFuturesPosition,
  placeFuturesOrder,
  closeFuturesPosition,
} from '@/api/futures';
import type { FuturesPosition, MarkPrice, FuturesSide, MarginMode } from '@/types';

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
  const [positionTab, setPositionTab] = useState<'open' | 'closed'>('open');
  const [countdown, setCountdown] = useState('--:--');

  useMarket(pair);

  const changePair = (p: string) => {
    setPair(p);
    setParams({ pair: p });
  };

  const loadData = async () => {
    try {
      const [mp, pos] = await Promise.all([
        getMarkPrice(pair),
        getFuturesPositions(),
      ]);
      setMarkPrice(mp);
      setPositions(pos);
    } catch {
      /* ignore */
    }
  };

  usePolling(loadData, 3000, [pair]);

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

  const resetForm = () => {
    setPrice('');
    setQuantity('');
    setTpPrice('');
    setSlPrice('');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!quantity || parseFloat(quantity) <= 0) return;
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
      }
      resetForm();
      await loadData();
    } catch {
      /* ignore */
    } finally {
      setSubmitting(false);
    }
  };

  const handleClose = async (id: string) => {
    try {
      await closeFuturesPosition(id);
      await loadData();
    } catch {
      /* ignore */
    }
  };

  const openPositions = positions.filter((p) => p.status === 'open');
  const closedPositions = positions.filter((p) => p.status === 'closed');

  return (
    <Layout>
      <div className="flex h-full flex-col gap-2 p-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <Select
              className="w-40"
              value={pair}
              onChange={(e) => changePair(e.target.value)}
              options={SUPPORTED_PAIRS.map((p) => ({ value: p, label: p }))}
            />
            <div className="flex items-center gap-4">
              <span className="text-xl font-semibold font-mono tabular-nums">
                {formatPrice(markPrice?.mark_price, 2)}
              </span>
              <div className="hidden sm:flex items-center gap-4 text-xs text-nexa-400">
                <div>
                  <span className="block text-nexa-500">Index</span>
                  <span className="font-mono">{formatPrice(markPrice?.index_price, 2)}</span>
                </div>
                <div>
                  <span className="block text-nexa-500">Funding</span>
                  <span className={cls('font-mono', changeColorClass(markPrice?.funding_rate))}>
                    {markPrice?.funding_rate ? `${Number(markPrice.funding_rate) >= 0 ? '+' : ''}${(Number(markPrice.funding_rate) * 100).toFixed(4)}%` : '--'}
                  </span>
                </div>
                <div>
                  <span className="block text-nexa-500">Next</span>
                  <span className="font-mono">{countdown}</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="grid flex-1 grid-cols-1 gap-2 lg:grid-cols-12">
          <div className="lg:col-span-3 flex flex-col gap-2">
            <div className="h-1/2 min-h-[200px]">
              <OrderbookPanel pair={pair} compact />
            </div>
            <div className="h-1/2 min-h-[200px]">
              <RecentTrades pair={pair} />
            </div>
          </div>

          <div className="lg:col-span-6 flex min-h-[300px] flex-col">
            <ChartPanel pair={pair} />
          </div>

          <div className="lg:col-span-3 flex flex-col gap-2">
            <Card title="Futures Order">
              <form onSubmit={handleSubmit} className="space-y-3 p-3">
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setSide('long')}
                    className={cls(
                      'rounded py-2 text-sm font-medium transition-colors',
                      side === 'long' ? 'bg-up text-white' : 'bg-nexa-800 text-nexa-300 hover:bg-nexa-700'
                    )}
                  >
                    Long
                  </button>
                  <button
                    type="button"
                    onClick={() => setSide('short')}
                    className={cls(
                      'rounded py-2 text-sm font-medium transition-colors',
                      side === 'short' ? 'bg-down text-white' : 'bg-nexa-800 text-nexa-300 hover:bg-nexa-700'
                    )}
                  >
                    Short
                  </button>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-nexa-300">Margin Mode</span>
                  <div className="flex rounded border border-nexa-700">
                    <button
                      type="button"
                      onClick={() => setMarginMode('isolated')}
                      className={cls(
                        'px-3 py-1 text-xs font-medium transition-colors',
                        marginMode === 'isolated' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                      )}
                    >
                      Isolated
                    </button>
                    <button
                      type="button"
                      onClick={() => setMarginMode('cross')}
                      className={cls(
                        'px-3 py-1 text-xs font-medium transition-colors',
                        marginMode === 'cross' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                      )}
                    >
                      Cross
                    </button>
                  </div>
                </div>

                <div>
                  <div className="mb-1 flex items-center justify-between text-xs text-nexa-300">
                    <span>Leverage</span>
                    <span className="font-mono text-accent">{leverage}x</span>
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
                </div>

                <div className="flex rounded border border-nexa-700">
                  <button
                    type="button"
                    onClick={() => setOrderType('market')}
                    className={cls(
                      'flex-1 px-3 py-1.5 text-xs font-medium transition-colors',
                      orderType === 'market' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                    )}
                  >
                    Market
                  </button>
                  <button
                    type="button"
                    onClick={() => setOrderType('limit')}
                    className={cls(
                      'flex-1 px-3 py-1.5 text-xs font-medium transition-colors',
                      orderType === 'limit' ? 'bg-accent text-nexa-950' : 'text-nexa-300 hover:bg-nexa-800'
                    )}
                  >
                    Limit
                  </button>
                </div>

                {orderType === 'limit' && (
                  <Input
                    label="Price"
                    type="number"
                    step="0.01"
                    placeholder="0.00"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    required
                  />
                )}

                <Input
                  label="Quantity"
                  type="number"
                  step="0.0001"
                  placeholder="0.00"
                  value={quantity}
                  onChange={(e) => setQuantity(e.target.value)}
                  required
                />

                <div className="grid grid-cols-2 gap-2">
                  <Input
                    label="TP"
                    type="number"
                    step="0.01"
                    placeholder="Take Profit"
                    value={tpPrice}
                    onChange={(e) => setTpPrice(e.target.value)}
                  />
                  <Input
                    label="SL"
                    type="number"
                    step="0.01"
                    placeholder="Stop Loss"
                    value={slPrice}
                    onChange={(e) => setSlPrice(e.target.value)}
                  />
                </div>

                <div className="space-y-1 rounded bg-nexa-900 p-2 text-xs text-nexa-300">
                  <div className="flex justify-between">
                    <span>Est. Margin</span>
                    <span className="font-mono">{estimatedMargin > 0 ? formatPrice(estimatedMargin, 2) : '--'} USDT</span>
                  </div>
                  <div className="flex justify-between">
                    <span>Est. Liq. Price</span>
                    <span className="font-mono">{estimatedLiq > 0 ? formatPrice(estimatedLiq, 2) : '--'}</span>
                  </div>
                </div>

                <Button
                  type="submit"
                  variant={side === 'long' ? 'success' : 'danger'}
                  className="w-full"
                  isLoading={submitting}
                >
                  Open {side === 'long' ? 'Long' : 'Short'}
                </Button>
              </form>
            </Card>

            <Card className="flex min-h-[240px] flex-1 flex-col" title="Positions">
              <div className="flex border-b border-nexa-700">
                <button
                  className={cls(
                    'flex-1 px-3 py-1.5 text-xs font-medium transition-colors',
                    positionTab === 'open' ? 'border-b-2 border-accent text-nexa-100' : 'text-nexa-400 hover:text-nexa-300'
                  )}
                  onClick={() => setPositionTab('open')}
                >
                  Open ({openPositions.length})
                </button>
                <button
                  className={cls(
                    'flex-1 px-3 py-1.5 text-xs font-medium transition-colors',
                    positionTab === 'closed' ? 'border-b-2 border-accent text-nexa-100' : 'text-nexa-400 hover:text-nexa-300'
                  )}
                  onClick={() => setPositionTab('closed')}
                >
                  Closed ({closedPositions.length})
                </button>
              </div>

              <div className="flex-1 overflow-auto p-2">
                {positionTab === 'open' && (
                  <div className="space-y-2">
                    {openPositions.map((p) => (
                      <div key={p.id} className="rounded border border-nexa-700 bg-nexa-900 p-2 text-xs">
                        <div className="mb-2 flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-nexa-100">{p.pair}</span>
                            <Badge color={p.side === 'long' ? 'up' : 'down'}>{p.side.toUpperCase()}</Badge>
                            <span className="text-nexa-400">{p.leverage}x</span>
                          </div>
                          <Button size="sm" variant="secondary" onClick={() => handleClose(p.id)}>
                            Close
                          </Button>
                        </div>
                        <div className="grid grid-cols-2 gap-y-1 text-nexa-300">
                          <div>Size: <span className="font-mono text-nexa-100">{formatQty(p.quantity, 6)}</span></div>
                          <div>Entry: <span className="font-mono text-nexa-100">{formatPrice(p.entry_price, 2)}</span></div>
                          <div>Mark: <span className="font-mono text-nexa-100">{formatPrice(p.mark_price, 2)}</span></div>
                          <div>Liq: <span className="font-mono text-nexa-100">{formatPrice(p.liq_price, 2)}</span></div>
                          <div className="col-span-2">
                            PnL:{' '}
                            <span className={cls('font-mono', changeColorClass(p.pnl))}>
                              {formatPrice(p.pnl, 2)} ({Number(p.pnl_pct || 0) >= 0 ? '+' : ''}{formatPrice(p.pnl_pct, 2)}%)
                            </span>
                          </div>
                        </div>
                      </div>
                    ))}
                    {openPositions.length === 0 && (
                      <div className="py-6 text-center text-nexa-500">No open positions</div>
                    )}
                  </div>
                )}

                {positionTab === 'closed' && (
                  <div className="space-y-2">
                    {closedPositions.map((p) => (
                      <div key={p.id} className="rounded border border-nexa-700 bg-nexa-900 p-2 text-xs">
                        <div className="mb-2 flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-nexa-100">{p.pair}</span>
                            <Badge color={p.side === 'long' ? 'up' : 'down'}>{p.side.toUpperCase()}</Badge>
                            <span className="text-nexa-400">{p.leverage}x</span>
                          </div>
                        </div>
                        <div className="grid grid-cols-2 gap-y-1 text-nexa-300">
                          <div>Size: <span className="font-mono text-nexa-100">{formatQty(p.quantity, 6)}</span></div>
                          <div>Entry: <span className="font-mono text-nexa-100">{formatPrice(p.entry_price, 2)}</span></div>
                          <div className="col-span-2">
                            PnL:{' '}
                            <span className={cls('font-mono', changeColorClass(p.pnl))}>
                              {formatPrice(p.pnl, 2)} ({Number(p.pnl_pct || 0) >= 0 ? '+' : ''}{formatPrice(p.pnl_pct, 2)}%)
                            </span>
                          </div>
                        </div>
                      </div>
                    ))}
                    {closedPositions.length === 0 && (
                      <div className="py-6 text-center text-nexa-500">No closed positions</div>
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
