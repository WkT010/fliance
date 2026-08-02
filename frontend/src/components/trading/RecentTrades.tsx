import { useTranslation } from 'react-i18next';
import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { formatPrice, formatQty, formatTime, cls } from '@/utils/format';

export function RecentTrades({ pair }: { pair: string }) {
  const { t } = useTranslation();
  const trades = useMarketStore((s) => s.trades);
  return (
    <Card className="flex h-full flex-col" title={`${t('trading.trades')} ${pair}`}>
      <div className="flex justify-between px-3 pt-2 text-xs text-nexa-400">
        <span>{t('trading.price')}</span>
        <span>{t('trading.qty')}</span>
        <span>{t('trading.time')}</span>
      </div>
      <div className="flex-1 overflow-auto px-3 py-2">
        {trades.slice(0, 50).map((tr, i) => (
          <div key={i} className="flex justify-between py-0.5 text-xs font-mono">
            <span className={cls(tr.side === 'buy' ? 'text-up' : 'text-down')}>{formatPrice(tr.price, 2)}</span>
            <span className="text-nexa-300">{formatQty(tr.quantity, 6)}</span>
            <span className="text-nexa-500">{formatTime(tr.time)}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}
