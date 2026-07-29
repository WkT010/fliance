import { useRef, useEffect, useState, useCallback } from 'react';
import { useMarketStore } from '@/store/marketStore';
import { Card } from '../common/Card';
import { formatPrice, formatQty } from '@/utils/format';

interface DepthPoint {
  price: number;
  cumulative: number;
}

interface HoverInfo {
  x: number;
  y: number;
  price: number;
  bidCum: number;
  askCum: number;
}

export function DepthChart({ pair, compact }: { pair: string; compact?: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const ob = useMarketStore((s) => s.orderbook);
  const [hover, setHover] = useState<HoverInfo | null>(null);
  const [dim, setDim] = useState({ w: 600, h: 350 });

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const { width, height } = e.contentRect;
        if (width > 0 && height > 0) setDim({ w: Math.floor(width), h: Math.floor(height) });
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const buildDepth = useCallback(() => {
    if (!ob) return { bids: [] as DepthPoint[], asks: [] as DepthPoint[], maxCum: 0, minP: 0, maxP: 0, midP: 0 };
    const bidLevels = (ob.bids || []).map((l) => ({ price: parseFloat(l.price), qty: parseFloat(l.quantity) })).filter((l) => l.price > 0 && l.qty > 0).sort((a, b) => b.price - a.price);
    const askLevels = (ob.asks || []).map((l) => ({ price: parseFloat(l.price), qty: parseFloat(l.quantity) })).filter((l) => l.price > 0 && l.qty > 0).sort((a, b) => a.price - b.price);
    let cum = 0; const bids = bidLevels.map((l) => { cum += l.qty; return { price: l.price, cumulative: cum }; });
    cum = 0; const asks = askLevels.map((l) => { cum += l.qty; return { price: l.price, cumulative: cum }; });
    const allPrices = [...bidLevels.map((l) => l.price), ...askLevels.map((l) => l.price)];
    if (allPrices.length === 0) return { bids: [], asks: [], maxCum: 0, minP: 0, maxP: 0, midP: 0 };
    const minP = Math.min(...allPrices); const maxP = Math.max(...allPrices);
    const maxCum = Math.max(bids.length > 0 ? bids[bids.length - 1].cumulative : 0, asks.length > 0 ? asks[asks.length - 1].cumulative : 0, 0.0001);
    const bestBid = bidLevels.length > 0 ? bidLevels[0].price : 0;
    const bestAsk = askLevels.length > 0 ? askLevels[0].price : 0;
    const midP = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : (minP + maxP) / 2;
    const padding = (maxP - minP) * 0.15 || minP * 0.01;
    return { bids, asks, maxCum, minP: minP - padding, maxP: maxP + padding, midP };
  }, [ob]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    const w = dim.w; const h = dim.h;
    canvas.width = w * dpr; canvas.height = h * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);
    const { bids, asks, maxCum, minP, maxP, midP } = buildDepth();
    if (bids.length === 0 && asks.length === 0) {
      ctx.fillStyle = '#5d6b7a'; ctx.font = '14px Inter, sans-serif'; ctx.textAlign = 'center';
      ctx.fillText('Waiting for orderbook data...', w / 2, h / 2);
      return;
    }
    const ml = 65; const mr = 12; const mt = 20; const mb = 40;
    const chartW = w - ml - mr; const chartH = h - mt - mb;
    const xPx = (price: number) => ml + ((price - minP) / (maxP - minP)) * chartW;
    const yPx = (cum: number) => mt + chartH - (cum / maxCum) * chartH;
    ctx.strokeStyle = '#1e242c'; ctx.lineWidth = 1;
    for (let i = 0; i <= 5; i++) {
      const y = mt + (chartH / 5) * i;
      ctx.beginPath(); ctx.moveTo(ml, y); ctx.lineTo(w - mr, y); ctx.stroke();
      ctx.fillStyle = '#5d6b7a'; ctx.font = '10px JetBrains Mono, monospace'; ctx.textAlign = 'right';
      ctx.fillText(formatQty(maxCum - (maxCum / 5) * i, 2), ml - 6, y + 4);
    }
    for (let i = 0; i <= 6; i++) {
      const x = ml + (chartW / 6) * i;
      ctx.beginPath(); ctx.moveTo(x, mt); ctx.lineTo(x, h - mb); ctx.stroke();
      ctx.fillStyle = '#5d6b7a'; ctx.font = '10px JetBrains Mono, monospace'; ctx.textAlign = 'center';
      ctx.fillText(formatPrice(minP + (maxP - minP) * (i / 6), 2), x, h - mb + 18);
    }
    if (asks.length > 0) {
      ctx.beginPath(); ctx.moveTo(xPx(asks[0].price), h - mb);
      for (const p of asks) ctx.lineTo(xPx(p.price), yPx(p.cumulative));
      ctx.lineTo(xPx(asks[asks.length - 1].price), h - mb); ctx.closePath();
      ctx.fillStyle = 'rgba(246, 70, 93, 0.15)'; ctx.fill();
      ctx.strokeStyle = '#f6465d'; ctx.lineWidth = 1.5; ctx.beginPath();
      for (let i = 0; i < asks.length; i++) { const px = xPx(asks[i].price); const py = yPx(asks[i].cumulative); if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py); }
      ctx.stroke();
    }
    if (bids.length > 0) {
      ctx.beginPath(); ctx.moveTo(xPx(bids[0].price), h - mb);
      for (const p of bids) ctx.lineTo(xPx(p.price), yPx(p.cumulative));
      ctx.lineTo(xPx(bids[bids.length - 1].price), h - mb); ctx.closePath();
      ctx.fillStyle = 'rgba(14, 203, 129, 0.15)'; ctx.fill();
      ctx.strokeStyle = '#0ecb81'; ctx.lineWidth = 1.5; ctx.beginPath();
      for (let i = 0; i < bids.length; i++) { const px = xPx(bids[i].price); const py = yPx(bids[i].cumulative); if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py); }
      ctx.stroke();
    }
    const midX = xPx(midP);
    ctx.strokeStyle = '#f0b90b'; ctx.lineWidth = 1; ctx.setLineDash([4, 4]);
    ctx.beginPath(); ctx.moveTo(midX, mt); ctx.lineTo(midX, h - mb); ctx.stroke(); ctx.setLineDash([]);
    ctx.fillStyle = '#f0b90b'; ctx.font = '11px JetBrains Mono, monospace'; ctx.textAlign = 'center';
    ctx.fillText(formatPrice(midP, 2), midX, mt - 4);
    ctx.fillStyle = '#8b97a8'; ctx.font = '11px Inter, sans-serif'; ctx.textAlign = 'center';
    ctx.fillText('Price', w / 2, h - 2);
    ctx.save(); ctx.translate(12, mt + chartH / 2); ctx.rotate(-Math.PI / 2); ctx.textAlign = 'center';
    ctx.fillStyle = '#8b97a8'; ctx.font = '11px Inter, sans-serif'; ctx.fillText('Cumulative Depth', 0, 0); ctx.restore();
    ctx.font = '11px Inter, sans-serif'; ctx.textAlign = 'left';
    ctx.fillStyle = '#0ecb81'; ctx.fillRect(w - mr - 100, mt + 6, 10, 10);
    ctx.fillText('Bids', w - mr - 86, mt + 15);
    ctx.fillStyle = '#f6465d'; ctx.fillRect(w - mr - 52, mt + 6, 10, 10);
    ctx.fillText('Asks', w - mr - 38, mt + 15);
  }, [dim, ob, buildDepth]);

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left; const my = e.clientY - rect.top;
    const ml = 65; const mr = 12; const mt = 20; const mb = 40;
    const w = dim.w; const h = dim.h; const chartW = w - ml - mr;
    if (mx < ml || mx > w - mr || my < mt || my > h - mb) { setHover(null); return; }
    const { bids, asks, minP, maxP } = buildDepth();
    const price = minP + ((mx - ml) / chartW) * (maxP - minP);
    let bidCum = 0; for (const b of bids) { if (b.price >= price) bidCum = b.cumulative; }
    let askCum = 0; for (const a of asks) { if (a.price <= price) askCum = a.cumulative; }
    setHover({ x: mx, y: my, price, bidCum, askCum });
  }, [dim, buildDepth]);

  const handleMouseLeave = useCallback(() => setHover(null), []);
  const title = compact ? undefined : `Depth Chart ${pair}`;

  return (
    <Card className="flex h-full flex-col" title={title}>
      <div ref={containerRef} className="relative flex-1 min-h-0">
        <canvas ref={canvasRef} className="h-full w-full cursor-crosshair" onMouseMove={handleMouseMove} onMouseLeave={handleMouseLeave} />
        {hover && (
          <div className="pointer-events-none absolute z-10 rounded border border-nexa-600 bg-nexa-900/95 px-3 py-2 text-xs shadow-lg" style={{ left: Math.min(hover.x + 14, dim.w - 160), top: Math.max(hover.y - 80, 4) }}>
            <div className="mb-1 font-medium text-nexa-100">Price: {formatPrice(hover.price, 2)}</div>
            <div className="flex items-center gap-2"><span className="h-2 w-2 rounded-full bg-up" /><span className="text-nexa-300">Bid Depth:</span><span className="text-up">{formatQty(hover.bidCum, 4)}</span></div>
            <div className="flex items-center gap-2"><span className="h-2 w-2 rounded-full bg-down" /><span className="text-nexa-300">Ask Depth:</span><span className="text-down">{formatQty(hover.askCum, 4)}</span></div>
            {hover.bidCum > 0 && hover.askCum > 0 && <div className="mt-0.5 border-t border-nexa-700 pt-0.5 text-nexa-400">Bid/Ask Ratio: {(hover.bidCum / Math.max(hover.askCum, 0.0001)).toFixed(2)}x</div>}
          </div>
        )}
      </div>
    </Card>
  );
}