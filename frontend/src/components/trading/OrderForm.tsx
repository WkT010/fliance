import { useState } from 'react';
import { placeOrder } from '@/api/order';
import { Button } from '../common/Button';
import { Input } from '../common/Input';
import { Select } from '../common/Select';
import { Badge } from '../common/Badge';
import { cls } from '@/utils/format';
import type { OrderSide, OrderType } from '@/types';

export function OrderForm({ pair }: { pair: string }) {
  const [side, setSide] = useState<OrderSide>('buy');
  const [type, setType] = useState<OrderType>('limit');
  const [price, setPrice] = useState('');
  const [stopPrice, setStopPrice] = useState('');
  const [quantity, setQuantity] = useState('');
  const [tpSlEnabled, setTpSlEnabled] = useState(false);
  const [tpPrice, setTpPrice] = useState('');
  const [slPrice, setSlPrice] = useState('');
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  const needsPrice = ['limit', 'post_only', 'stop_limit', 'iceberg'].includes(type);
  const needsStop = ['stop_loss', 'stop_limit'].includes(type);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setStatus(null);
    try {
      await placeOrder({
        pair, side, type,
        price: needsPrice ? price : undefined,
        stop_price: needsStop ? stopPrice : undefined,
        quantity,
        tp_price: tpSlEnabled && tpPrice.trim() ? tpPrice : undefined,
        sl_price: tpSlEnabled && slPrice.trim() ? slPrice : undefined,
      });
      setStatus('Order placed');
      setPrice('');
      setStopPrice('');
      setQuantity('');
      setTpPrice('');
      setSlPrice('');
      setTpSlEnabled(false);
    } catch (err: unknown) {
      setStatus(err instanceof Error ? err.message : 'Failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={submit} className="flex h-full flex-col gap-3 rounded border border-nexa-700 bg-nexa-800/50 p-4">
      <div className="flex gap-2">
        <button type="button" onClick={() => setSide('buy')} className={cls('flex-1 rounded py-2 text-sm font-medium transition-colors', side === 'buy' ? 'bg-up text-white' : 'bg-nexa-700 text-nexa-300')}>Buy</button>
        <button type="button" onClick={() => setSide('sell')} className={cls('flex-1 rounded py-2 text-sm font-medium transition-colors', side === 'sell' ? 'bg-down text-white' : 'bg-nexa-700 text-nexa-300')}>Sell</button>
      </div>
      <Select label="Type" value={type} onChange={(e) => setType(e.target.value as OrderType)}
        options={[{ value: 'limit', label: 'Limit' }, { value: 'market', label: 'Market' }, { value: 'ioc', label: 'IOC' }, { value: 'fok', label: 'FOK' }, { value: 'post_only', label: 'Post Only' }]} />
      {needsPrice && <Input label="Price" type="number" step="0.01" value={price} onChange={(e) => setPrice(e.target.value)} required />}
      {needsStop && <Input label="Stop Price" type="number" step="0.01" value={stopPrice} onChange={(e) => setStopPrice(e.target.value)} required />}
      <label className="flex cursor-pointer items-center gap-2 text-sm text-nexa-300">
        <input
          type="checkbox"
          checked={tpSlEnabled}
          onChange={(e) => setTpSlEnabled(e.target.checked)}
          className="h-4 w-4 rounded border-nexa-700 bg-nexa-900 text-accent accent-accent focus:ring-1 focus:ring-accent/50"
        />
        Enable TP / SL
      </label>
      {tpSlEnabled && (
        <>
          <Input label="TP Price" type="number" step="0.01" value={tpPrice} onChange={(e) => setTpPrice(e.target.value)} />
          <Input label="SL Price" type="number" step="0.01" value={slPrice} onChange={(e) => setSlPrice(e.target.value)} />
        </>
      )}
      <Input label="Quantity" type="number" step="0.000001" value={quantity} onChange={(e) => setQuantity(e.target.value)} required />
      <Button type="submit" variant={side === 'buy' ? 'success' : 'danger'} isLoading={loading} className="mt-auto">{side === 'buy' ? 'Buy' : 'Sell'} {pair.split('/')[0]}</Button>
      {status && <Badge color={status === 'Order placed' ? 'up' : 'down'}>{status}</Badge>}
    </form>
  );
}
