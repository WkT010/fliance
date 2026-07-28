import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { formatPrice, formatQty } from '@/utils/format';

export function OrderbookPanel({ pair }: { pair: string }) {
  const ob = useMarketStore((s) => s.orderbook);
  const asks = (ob?.asks || []).slice(0, 8).reverse();
  const bids = (ob?.bids || []).slice(0, 8);

  const maxQty = Math.max(
    ...asks.map((a) => parseFloat(a.quantity)),
    ...bids.map((b) => parseFloat(b.quantity)),
    0.0001
  );

  const Row = ({ level, side, max }: { level: { price: string; quantity: string }; side: 'ask' | 'bid'; max: number }) => {
    const qty = parseFloat(level.quantity);
    const width = `${(qty / max) * 100}%`;
    return (
      <div className="relative flex justify-between py-0.5 text-xs">
        <div className="absolute inset-y-0 right-0 opacity-10" style={{ width, backgroundColor: side === 'bid' ? '#0ecb81' : '#f6465d' }} />
        <span className={side === 'bid' ? 'text-up' : 'text-down'}>{formatPrice(level.price, 2)}</span>
        <span className="text-nexa-300">{formatQty(level.quantity, 6)}</span>
      </div>
    );
  };

  return (
    <Card className="flex h-full flex-col" title={`Order Book ${pair}`}>
      <div className="flex justify-between px-3 pt-2 text-xs text-nexa-400">
        <span>Price</span>
        <span>Quantity</span>
      </div>
      <div className="flex-1 overflow-auto px-3 py-2">
        <div className="mb-1">
          {asks.map((a, i) => <Row key={`ask-${i}`} level={a} side="ask" max={maxQty} />)}
        </div>
        <div className="border-y border-nexa-700 py-1 text-center text-sm font-medium text-nexa-100">
          {formatPrice(ob?.bids[0]?.price || ob?.asks[0]?.price, 2)}
        </div>
        <div className="mt-1">
          {bids.map((b, i) => <Row key={`bid-${i}`} level={b} side="bid" max={maxQty} />)}
        </div>
      </div>
    </Card>
  );
}
