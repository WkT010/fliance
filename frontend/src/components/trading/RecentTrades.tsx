import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { formatPrice, formatQty, formatTime, cls } from '@/utils/format';

export function RecentTrades({ pair }: { pair: string }) {
  const trades = useMarketStore((s) => s.trades);
  return (
    <Card className="flex h-full flex-col" title={`Trades ${pair}`}>
      <div className="flex justify-between px-3 pt-2 text-xs text-nexa-400">
        <span>Price</span>
        <span>Qty</span>
        <span>Time</span>
      </div>
      <div className="flex-1 overflow-auto px-3 py-2">
        {trades.slice(0, 50).map((t, i) => (
          <div key={i} className="flex justify-between py-0.5 text-xs font-mono">
            <span className={cls(t.side === 'buy' ? 'text-up' : 'text-down')}>{formatPrice(t.price, 2)}</span>
            <span className="text-nexa-300">{formatQty(t.quantity, 6)}</span>
            <span className="text-nexa-500">{formatTime(t.time)}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}
