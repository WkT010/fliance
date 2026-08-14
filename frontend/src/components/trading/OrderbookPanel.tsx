import { useTranslation } from 'react-i18next';
import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { formatPrice, formatQty } from '@/utils/format';

export function OrderbookPanel({ pair, compact }: { pair: string; compact?: boolean }) {
  const { t } = useTranslation();
  const ob = useMarketStore((s) => s.orderbook);
  const asks = (ob?.asks || []).slice(0, 8).reverse();
  const bids = (ob?.bids || []).slice(0, 8);
  const maxQty = Math.max(...asks.map((a) => parseFloat(a.quantity)), ...bids.map((b) => parseFloat(b.quantity)), 0.0001);

  const Row = ({ level, side, max }: { level: { price: string; quantity: string }; side: 'ask' | 'bid'; max: number }) => {
    const qty = parseFloat(level.quantity);
    const width = `${(qty / max) * 100}%`;
    return (
      <div className="relative flex justify-between py-0.5 text-xs">
        <div className="absolute inset-y-0 right-0 opacity-10" style={{ width, backgroundColor: side === 'bid' ? '#0ecb81' : '#f6465d' }} />
        <span className={'font-mono ' + (side === 'bid' ? 'text-up' : 'text-down')}>{formatPrice(level.price, 2)}</span>
        <span className="font-mono text-nexa-300">{formatQty(level.quantity, 6)}</span>
      </div>
    );
  };

  const content = (
    <div className="flex h-full min-h-[360px] flex-col">
      <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/40 px-4 py-2.5">
        <div className="text-sm font-semibold text-nexa-100">{t('trading.orderBook')}</div>
        <span className="text-xs text-nexa-500">{pair}</span>
      </div>
      <div className="flex justify-between px-3 pt-2 text-xs text-nexa-400"><span>{t('trading.price')}</span><span>{t('trading.qty')}</span></div>
      <div className="flex-1 overflow-auto px-3 py-2">
        <div className="mb-1">{asks.map((a, i) => <Row key={`ask-${i}`} level={a} side="ask" max={maxQty} />)}</div>
        <div className="border-y border-nexa-700 py-1 text-center text-sm font-medium text-nexa-100">{formatPrice(bids[0]?.price || asks[0]?.price, 2)}</div>
        <div className="mt-1">{bids.map((b, i) => <Row key={`bid-${i}`} level={b} side="bid" max={maxQty} />)}</div>
      </div>
    </div>
  );

  if (compact) return content;
  return <Card className="flex h-full flex-col" title={`${t('trading.orderBook')} ${pair}`}>{content}</Card>;
}
