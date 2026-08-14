import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { placeOrder } from '@/api/order';
import { getApiErrorMessage } from '@/api/client';
import { Button } from '../common/Button';
import { Input } from '../common/Input';
import { Select } from '../common/Select';
import { cls, formatPrice } from '@/utils/format';
import { toast } from '@/store/toastStore';
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
    if (loading) return;
    setLoading(true);
    try {
      await placeOrder({
        pair, side, type,
        price: needsPrice ? price : undefined,
        stop_price: needsStop ? stopPrice : undefined,
        quantity,
        tp_price: tpSlEnabled && tpPrice.trim() ? tpPrice : undefined,
        sl_price: tpSlEnabled && slPrice.trim() ? slPrice : undefined,
      });
      toast.success(t('trading.orderPlaced'), `${side.toUpperCase()} ${pair.split('/')[0]}`);
      setPrice('');
      setStopPrice('');
      setQuantity('');
      setTpPrice('');
      setSlPrice('');
      setTpSlEnabled(false);
    } catch (err: unknown) {
      toast.error(getApiErrorMessage(err, t('trading.failed')), t('trading.failed'));
    } finally {
      setLoading(false);
    }
  };

  const base = pair.split('/')[0];
  const quote = pair.split('/')[1] ?? '';

  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-3 rounded-xl border border-nexa-700/70 bg-nexa-800/60 p-4 shadow-lg shadow-black/20"
    >
      {/* Buy / Sell segmented control */}
      <div className="flex gap-2 rounded-lg bg-nexa-900/80 p-1">
        <button
          type="button"
          onClick={() => setSide('buy')}
          className={cls(
            'flex-1 rounded-md py-2 text-sm font-semibold transition-all',
            side === 'buy'
              ? 'bg-up text-white shadow-md shadow-up/30'
              : 'text-nexa-300 hover:bg-nexa-800/60 hover:text-nexa-100'
          )}
        >
          {t('trading.buy')} {base}
        </button>
        <button
          type="button"
          onClick={() => setSide('sell')}
          className={cls(
            'flex-1 rounded-md py-2 text-sm font-semibold transition-all',
            side === 'sell'
              ? 'bg-down text-white shadow-md shadow-down/30'
              : 'text-nexa-300 hover:bg-nexa-800/60 hover:text-nexa-100'
          )}
        >
          {t('trading.sell')} {base}
        </button>
      </div>

      <Select
        label={t('trading.orderType')}
        value={type}
        onChange={(e) => setType(e.target.value as OrderType)}
        options={[
          { value: 'limit', label: t('trading.limit') },
          { value: 'market', label: t('trading.market') },
          { value: 'ioc', label: t('trading.ioc') },
          { value: 'fok', label: t('trading.fok') },
          { value: 'post_only', label: t('trading.postOnly') },
        ]}
      />

      {needsPrice && (
        <Input
          label={`${t('trading.price')} (${quote})`}
          type="number"
          step="0.01"
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          required
          suffix={quote}
        />
      )}
      {needsStop && (
        <Input
          label={t('trading.stopPrice')}
          type="number"
          step="0.01"
          value={stopPrice}
          onChange={(e) => setStopPrice(e.target.value)}
          required
        />
      )}

      {/* TP/SL toggle */}
      <label className="flex cursor-pointer select-none items-center gap-2 rounded-md border border-nexa-700/50 bg-nexa-900/40 px-3 py-2 text-sm text-nexa-300 transition-colors hover:border-nexa-600">
        <input
          type="checkbox"
          checked={tpSlEnabled}
          onChange={(e) => setTpSlEnabled(e.target.checked)}
          className="h-4 w-4 rounded border-nexa-700 bg-nexa-900 text-accent accent-accent focus:ring-1 focus:ring-accent/50"
        />
        <span className="font-medium">{t('trading.enableTpSl')}</span>
      </label>
      {tpSlEnabled && (
        <div className="grid grid-cols-2 gap-2">
          <Input
            label={`${t('trading.tp')} (${quote})`}
            type="number"
            step="0.01"
            value={tpPrice}
            onChange={(e) => setTpPrice(e.target.value)}
          />
          <Input
            label={`${t('trading.sl')} (${quote})`}
            type="number"
            step="0.01"
            value={slPrice}
            onChange={(e) => setSlPrice(e.target.value)}
          />
        </div>
      )}

      {/* Quantity */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium text-nexa-300">{t('trading.quantity')} ({base})</label>
          <button type="button" onClick={setMaxQty} className="text-xs font-semibold text-accent transition-colors hover:text-accent/80">
            {t('trading.max')}
          </button>
        </div>
        <Input
          type="number"
          step="0.000001"
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          required
          suffix={base}
        />
        <div className="grid grid-cols-4 gap-1">
          {[0.25, 0.5, 0.75, 1].map((pct) => (
            <button
              key={pct}
              type="button"
              onClick={() => setQtyFromPct(pct)}
              disabled={effectivePrice <= 0}
              className="rounded-md bg-nexa-700/70 px-1 py-1.5 text-xs font-medium text-nexa-200 transition-colors hover:bg-nexa-600 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {pct * 100}%
            </button>
          ))}
        </div>
      </div>

      {/* Notional */}
      <div className="flex items-center justify-between rounded-md bg-nexa-900/50 px-3 py-2 text-xs">
        <span className="text-nexa-400">{t('trading.estNotional')}</span>
        <span className="font-mono text-nexa-100">
          {notional > 0 ? `${formatPrice(String(notional), 2)} ${quote}` : '—'}
        </span>
      </div>

      <Button
        type="submit"
        variant={side === 'buy' ? 'success' : 'danger'}
        isLoading={loading}
        block
        className="mt-auto"
      >
        {side === 'buy' ? t('trading.buy') : t('trading.sell')} {base}
      </Button>
    </form>
  );
}
