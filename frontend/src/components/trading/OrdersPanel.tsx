import { useEffect, useState } from 'react';
import { listOrders, cancelOrder } from '@/api/order';
import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { Button } from '../common/Button';
import { Badge } from '../common/Badge';
import { formatPrice, formatQty, formatTime } from '@/utils/format';
import type { Order } from '@/types';

export function OrdersPanel({ pair }: { pair: string }) {
  const [orders, setOrders] = useState<Order[]>([]);
  const fills = useMarketStore((s) => s.fills);

  const load = async () => {
    try { setOrders(await listOrders(pair)); } catch { /* ignore */ }
  };

  useEffect(() => { load(); }, [pair]);
  useEffect(() => { load(); }, [fills.length]);

  const active = orders.filter((o) => ['new', 'partially_filled'].includes(o.status));

  return (
    <Card className="h-full" title="Open Orders">
      <div className="overflow-auto px-4 pb-4">
        <table className="w-full text-left text-xs">
          <thead className="text-nexa-400">
            <tr>
              <th className="py-2">Side</th>
              <th>Type</th>
              <th>Price</th>
              <th>Qty</th>
              <th>Filled</th>
              <th>Status</th>
              <th>Time</th>
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
                <td><Button size="sm" variant="danger" onClick={() => cancelOrder(o.id).then(load)}>Cancel</Button></td>
              </tr>
            ))}
            {active.length === 0 && <tr><td colSpan={8} className="py-4 text-center text-nexa-500">No open orders</td></tr>}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
