import { useEffect, useRef, useState } from 'react';
import { createChart, type IChartApi, type ISeriesApi, type CandlestickData, type Time } from 'lightweight-charts';
import { useMarketStore } from '@/store/marketStore';
import { getCandles } from '@/api/market';
import { INTERVALS } from '@/utils/constants';
import { Card } from '../common/Card';
import { Select } from '../common/Select';
import { formatPrice } from '@/utils/format';

export function ChartPanel({ pair }: { pair: string }) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null);
  const [interval, setInterval] = useState('15m');
  const tickers = useMarketStore((s) => s.tickers);

  useEffect(() => {
    if (!chartContainerRef.current) return;
    const chart = createChart(chartContainerRef.current, {
      layout: { background: { color: 'transparent' }, textColor: '#d4dbe3' },
      grid: { vertLines: { color: '#1e242c' }, horzLines: { color: '#1e242c' } },
      crosshair: { mode: 1 },
      rightPriceScale: { borderColor: '#2a313c' },
      timeScale: { borderColor: '#2a313c', timeVisible: true },
    });
    chartRef.current = chart;
    const series = chart.addCandlestickSeries({
      upColor: '#0ecb81', downColor: '#f6465d', borderUpColor: '#0ecb81', borderDownColor: '#f6465d',
      wickUpColor: '#0ecb81', wickDownColor: '#f6465d',
    });
    seriesRef.current = series;

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth, height: chartContainerRef.current.clientHeight });
      }
    };
    window.addEventListener('resize', handleResize);
    handleResize();

    return () => { window.removeEventListener('resize', handleResize); chart.remove(); };
  }, []);

  useEffect(() => {
    getCandles(pair, interval).then((data) => {
      const candles: CandlestickData<Time>[] = data.map((c) => ({
        time: Math.floor(c.timestamp / 1e6 / 1000) as Time,
        open: parseFloat(c.open),
        high: parseFloat(c.high),
        low: parseFloat(c.low),
        close: parseFloat(c.close),
      }));
      seriesRef.current?.setData(candles);
      chartRef.current?.timeScale().fitContent();
    });
  }, [pair, interval]);

  return (
    <Card className="flex h-full flex-col" title={
      <div className="flex items-center justify-between">
        <span>{pair} <span className="text-nexa-400">{formatPrice(tickers[pair]?.last, 2)}</span></span>
        <Select
          className="w-28"
          value={interval}
          onChange={(e) => setInterval(e.target.value)}
          options={INTERVALS.map((i) => ({ value: i, label: i }))}
        />
      </div>
    }>
      <div ref={chartContainerRef} className="flex-1 min-h-[300px]" />
    </Card>
  );
}
