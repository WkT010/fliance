import { useTranslation } from 'react-i18next';
import { useMarketStore } from '@/store/marketStore';
import { EmptyState } from '@/components/common/EmptyState';
import { formatPrice, formatQty, formatTime, cls } from '@/utils/format';

export function RecentTrades({ pair }: { pair: string }) {
  const { t } = useTranslation();
  const trades = useMarketStore((s) => s.trades);
  const filtered = trades.filter((tr) => !tr.pair || tr.pair === pair).slice(0, 50);
  return (
    <div className="flex h-full min-h-[360px] flex-col">
      <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/40 px-4 py-2.5">
        <div className="text-sm font-semibold text-nexa-100">{t('trading.trades')}</div>
        <span className="text-xs text-nexa-500">{pair}</span>
      </div>
      <div className="flex justify-between px-4 py-1.5 text-xs text-nexa-500">
        <span>{t('trading.price')}</span>
        <span>{t('trading.qty')}</span>
        <span>{t('trading.time')}</span>
      </div>
      <div className="flex-1 overflow-auto px-3">
        {filtered.length === 0 ? (
          <EmptyState title={t('trading.waitingOrderbook')} compact />
        ) : (
          filtered.map((tr, i) => (
            <div key={i} className="flex justify-between rounded px-1 py-0.5 text-xs font-mono transition-colors hover:bg-nexa-800/40">
              <span className={cls('font-semibold', tr.side === 'buy' ? 'text-up' : 'text-down')}>{formatPrice(tr.price, 2)}</span>
              <span className="text-nexa-300">{formatQty(tr.quantity, 6)}</span>
              <span className="text-nexa-500">{formatTime(tr.time)}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
