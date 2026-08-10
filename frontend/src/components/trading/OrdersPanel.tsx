import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listOrders, cancelOrder } from '@/api/order';
import { useMarketStore } from '@/store/marketStore';
import { EmptyState } from '@/components/common/EmptyState';
import { Button } from '@/components/common/Button';
import { Badge } from '@/components/common/Badge';
import { formatPrice, formatQty, formatTime, cls } from '@/utils/format';
import { toast } from '@/store/toastStore';
import type { Order } from '@/types';

export function OrdersPanel({ pair }: { pair: string }) {
  const { t } = useTranslation();
  const [orders, setOrders] = useState<Order[]>([]);
  const [cancellingId, setCancellingId] = useState<string | null>(null);
  const fills = useMarketStore((s) => s.fills);

  const load = async () => {
    try { setOrders(await listOrders(pair)); } catch { /* ignore */ }
  };

  useEffect(() => { load(); }, [pair]);
  useEffect(() => { load(); }, [fills.length]);

  const active = orders.filter((o) => ['new', 'partially_filled'].includes(o.status));

  const onCancel = async (id: string) => {
    if (cancellingId) return;
    setCancellingId(id);
    try {
      await cancelOrder(id);
      toast.success(t('trading.cancel'));
      await load();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : t('common.failed'));
    } finally {
      setCancellingId(null);
    }
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/40 px-4 py-2.5">
        <div className="text-sm font-semibold text-nexa-100">{t('trading.openOrders')}</div>
        {active.length > 0 && <Badge color="accent">{active.length}</Badge>}
      </div>
      <div className="flex-1 overflow-auto p-3">
        {active.length === 0 ? (
          <EmptyState title={t('trading.noOpenOrders')} compact />
        ) : (
          <table className="w-full text-left text-xs">
            <thead className="text-nexa-400">
              <tr>
                <th className="py-2 font-medium">{t('trading.side')}</th>
                <th className="font-medium">{t('trading.type')}</th>
                <th className="font-medium text-right">{t('trading.price')}</th>
                <th className="font-medium text-right">{t('trading.qty')}</th>
                <th className="font-medium text-right">{t('trading.filled')}</th>
                <th className="font-medium">{t('trading.status')}</th>
                <th className="font-medium">{t('trading.time')}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {active.map((o) => (
                <tr key={o.id} className="border-b border-nexa-800/50 transition-colors hover:bg-nexa-800/30">
                  <td className={cls('py-2 font-semibold', o.side === 'buy' ? 'text-up' : 'text-down')}>{o.side.toUpperCase()}</td>
                  <td className="text-nexa-300">{o.type}</td>
                  <td className="text-right font-mono text-nexa-100">{formatPrice(o.price, 2)}</td>
                  <td className="text-right font-mono text-nexa-100">{formatQty(o.quantity, 6)}</td>
                  <td className="text-right font-mono text-nexa-300">{formatQty(o.filled_qty || '0', 6)}</td>
                  <td><Badge color="accent" size="sm">{o.status}</Badge></td>
                  <td className="text-nexa-500">{formatTime(o.created_at)}</td>
                  <td className="text-right">
                    <Button size="sm" variant="danger" isLoading={cancellingId === o.id} onClick={() => onCancel(o.id)}>
                      {t('trading.cancel')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
