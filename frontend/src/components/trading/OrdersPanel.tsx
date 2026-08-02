import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listOrders, cancelOrder } from '@/api/order';
import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { Button } from '../common/Button';
import { Badge } from '../common/Badge';
import { formatPrice, formatQty, formatTime } from '@/utils/format';
import type { Order } from '@/types';

export function OrdersPanel({ pair }: { pair: string }) {
  const { t } = useTranslation();
  const [orders, setOrders] = useState<Order[]>([]);
  const fills = useMarketStore((s) => s.fills);

  const load = async () => {
    try { setOrders(await listOrders(pair)); } catch { /* ignore */ }
  };

  useEffect(() => { load(); }, [pair]);
  useEffect(() => { load(); }, [fills.length]);

  const active = orders.filter((o) => ['new', 'partially_filled'].includes(o.status));

  return (
    <Card className="h-full" title={t('trading.openOrders')}>
      <div className="overflow-auto px-4 pb-4">
        <table className="w-full text-left text-xs">
          <thead className="text-nexa-400">
            <tr>
              <th className="py-2">{t('trading.side')}</th>
              <th>{t('trading.type')}</th>
              <th>{t('trading.price')}</th>
              <th>{t('trading.qty')}</th>
              <th>{t('trading.filled')}</th>
              <th>{t('trading.status')}</th>
              <th>{t('trading.time')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {active.map((o) => (
              <tr key={o.id} className="border-b border-nexa-700/50">
                <td className={o.side === 'buy' ? 'text-up' : 'text-down'}>{o.side.toUpperCase()}</td>
                <td className="text-nexa-300">{o.type}</td>
                <td className="text-nexa-300">{formatPrice(o.price, 2)}</td>
                <td className="text-nexa-300">{formatQty(o.quantity, 6)}</td>
                <td className="text-nexa-300">{formatQty(o.filled_qty || '0', 6)}</td>
                <td><Badge color="accent">{o.status}</Badge></td>
                <td className="text-nexa-500">{formatTime(o.created_at)}</td>
                <td><Button size="sm" variant="danger" onClick={() => cancelOrder(o.id).then(load)}>{t('trading.cancel')}</Button></td>
              </tr>
            ))}
            {active.length === 0 && <tr><td colSpan={8} className="py-4 text-center text-nexa-500">{t('trading.noOpenOrders')}</td></tr>}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
