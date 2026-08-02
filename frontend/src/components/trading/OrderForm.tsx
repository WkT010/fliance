import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { placeOrder } from '@/api/order';
import { Button } from '../common/Button';
import { Input } from '../common/Input';
import { Select } from '../common/Select';
import { Badge } from '../common/Badge';
import { cls, formatPrice } from '@/utils/format';
import type { OrderSide, OrderType } from '@/types';

interface OrderFormProps {
  pair: string;
  maxNotional?: number;
  markPrice?: number | string;
}

export function OrderForm({ pair, maxNotional = 10000, markPrice }: OrderFormProps) {
  const { t } = useTranslation();
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

  const effectivePrice = useMemo(() => {
    const entered = needsPrice ? Number(price) : 0;
    const mark = Number(markPrice) || 0;
    return entered > 0 ? entered : mark;
  }, [needsPrice, price, markPrice]);

  const notional = useMemo(() => {
    const q = Number(quantity) || 0;
    return effectivePrice > 0 ? effectivePrice * q : 0;
  }, [effectivePrice, quantity]);

  const setQtyFromPct = (pct: number) => {
    if (effectivePrice <= 0 || maxNotional <= 0) return;
    const value = (maxNotional * pct) / effectivePrice;
    setQuantity(value > 0 ? value.toFixed(6) : '');
  };

  const setMaxQty = () => setQtyFromPct(1);

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
      setStatus(t('trading.orderPlaced'));
      setPrice('');
      setStopPrice('');
      setQuantity('');
      setTpPrice('');
      setSlPrice('');
      setTpSlEnabled(false);
    } catch (err: unknown) {
      setStatus(err instanceof Error ? err.message : t('trading.failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={submit} className="flex h-full flex-col gap-3 rounded border border-nexa-700 bg-nexa-800/50 p-4">
      <div className="flex gap-2">
        <button type="button" onClick={() => setSide('buy')} className={cls('flex-1 rounded py-2 text-sm font-medium transition-colors', side === 'buy' ? 'bg-up text-white' : 'bg-nexa-700 text-nexa-300')}>{t('trading.buy')}</button>
        <button type="button" onClick={() => setSide('sell')} className={cls('flex-1 rounded py-2 text-sm font-medium transition-colors', side === 'sell' ? 'bg-down text-white' : 'bg-nexa-700 text-nexa-300')}>{t('trading.sell')}</button>
      </div>
      <Select label={t('trading.type')} value={type} onChange={(e) => setType(e.target.value as OrderType)}
        options={[
          { value: 'limit', label: t('trading.limit') },
          { value: 'market', label: t('trading.market') },
          { value: 'ioc', label: t('trading.ioc') },
          { value: 'fok', label: t('trading.fok') },
          { value: 'post_only', label: t('trading.postOnly') },
        ]} />
      {needsPrice && <Input label={t('trading.price')} type="number" step="0.01" value={price} onChange={(e) => setPrice(e.target.value)} required />}
      {needsStop && <Input label={t('trading.stopPrice')} type="number" step="0.01" value={stopPrice} onChange={(e) => setStopPrice(e.target.value)} required />}
      <label className="flex cursor-pointer items-center gap-2 text-sm text-nexa-300">
        <input
          type="checkbox"
          checked={tpSlEnabled}
          onChange={(e) => setTpSlEnabled(e.target.checked)}
          className="h-4 w-4 rounded border-nexa-700 bg-nexa-900 text-accent accent-accent focus:ring-1 focus:ring-accent/50"
        />
        {t('trading.enableTpSl')}
      </label>
      {tpSlEnabled && (
        <>
          <Input label={t('trading.tpPrice')} type="number" step="0.01" value={tpPrice} onChange={(e) => setTpPrice(e.target.value)} />
          <Input label={t('trading.slPrice')} type="number" step="0.01" value={slPrice} onChange={(e) => setSlPrice(e.target.value)} />
        </>
      )}
      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium text-nexa-300">{t('trading.quantity')}</label>
          <button type="button" onClick={setMaxQty} className="text-xs font-medium text-accent hover:text-accent/80">{t('trading.max')}</button>
        </div>
        <Input type="number" step="0.000001" value={quantity} onChange={(e) => setQuantity(e.target.value)} required />
        <div className="grid grid-cols-4 gap-1">
          {[0.25, 0.5, 0.75, 1].map((pct) => (
            <button
              key={pct}
              type="button"
              onClick={() => setQtyFromPct(pct)}
              disabled={effectivePrice <= 0}
              className="rounded bg-nexa-700 px-1 py-1 text-xs font-medium text-nexa-200 transition-colors hover:bg-nexa-600 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {pct * 100}%
            </button>
          ))}
        </div>
      </div>
      <div className="flex items-center justify-between text-xs text-nexa-400">
        <span>{t('trading.estNotional')}</span>
        <span className="font-mono text-nexa-200">{notional > 0 ? `${formatPrice(String(notional), 2)} USDT` : '--'}</span>
      </div>
      <Button type="submit" variant={side === 'buy' ? 'success' : 'danger'} isLoading={loading} className="mt-auto">{side === 'buy' ? t('trading.buy') : t('trading.sell')} {pair.split('/')[0]}</Button>
      {status && <Badge color={status === t('trading.orderPlaced') ? 'up' : 'down'}>{status}</Badge>}
    </form>
  );
}
