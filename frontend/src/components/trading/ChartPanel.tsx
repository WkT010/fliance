import { useEffect, useRef, useState, useMemo } from 'react';
import {
  createChart,
  type IChartApi,
  type ISeriesApi,
  type CandlestickData,
  type HistogramData,
  type LineData,
  type Time,
  type AreaData,
  type BarData,
  LineStyle,
  CrosshairMode,
  PriceScaleMode,
} from 'lightweight-charts';
import { useMarketStore } from '@/store/marketStore';
import { getCandles } from '@/api/market';
import { INTERVALS } from '@/utils/constants';
import { Card } from '../common/Card';
import { Select } from '../common/Select';
import { Button } from '../common/Button';
import { formatPrice } from '@/utils/format';
import {
  sma, ema, rsi, bollinger, heikinAshi, macd, kdj, stochastic, vwap, volumeSma, atr, williamsR, cci,
  type Candle,
} from '@/utils/indicators';

type ChartType = 'candle' | 'bar' | 'line' | 'area' | 'heikin';

interface IndicatorDef {
  id: string;
  label: string;
  fn: (data: Candle[]) => { time: number; value: number }[];
  color: string;
}

const MAIN_INDICATORS: IndicatorDef[] = [
  { id: 'sma7', label: 'MA7', fn: (d) => sma(d, 7), color: '#f59e0b' },
  { id: 'sma25', label: 'MA25', fn: (d) => sma(d, 25), color: '#3b82f6' },
  { id: 'sma99', label: 'MA99', fn: (d) => sma(d, 99), color: '#a855f7' },
  { id: 'ema12', label: 'EMA12', fn: (d) => ema(d, 12), color: '#22c55e' },
  { id: 'ema26', label: 'EMA26', fn: (d) => ema(d, 26), color: '#ef4444' },
  { id: 'vwap', label: 'VWAP', fn: (d) => vwap(d), color: '#f97316' },
];

interface SubPanelConfig {
  id: string;
  label: string;
  height: number;
}

const SUB_PANELS: SubPanelConfig[] = [
  { id: 'rsi', label: 'RSI', height: 110 },
  { id: 'macd', label: 'MACD', height: 110 },
  { id: 'kdj', label: 'KDJ', height: 110 },
  { id: 'stochastic', label: 'Stochastic', height: 110 },
  { id: 'atr', label: 'ATR', height: 90 },
  { id: 'williamsR', label: 'Williams %R', height: 90 },
  { id: 'cci', label: 'CCI', height: 90 },
];

export function ChartPanel({ pair }: { pair: string }) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const mainSeriesRef = useRef<ISeriesApi<'Candlestick'> | ISeriesApi<'Bar'> | ISeriesApi<'Line'> | ISeriesApi<'Area'> | null>(null);
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null);
  const volumeMaSeriesRef = useRef<ISeriesApi<'Line'> | null>(null);
  const indicatorSeriesRef = useRef<ISeriesApi<'Line'>[]>([]);
  const bollingerSeriesRef = useRef<ISeriesApi<'Line'>[]>([]);

  const subRefs = useRef<Record<string, {
    container: HTMLDivElement | null;
    chart: IChartApi | null;
    series: ISeriesApi<'Line'>[];
    histogram?: ISeriesApi<'Histogram'> | null;
  }>>({});

  const [interval, setInterval] = useState('15m');
  const [chartType, setChartType] = useState<ChartType>('candle');
  const [activeIndicators, setActiveIndicators] = useState<string[]>(['sma7', 'sma25']);
  const [showVolume, setShowVolume] = useState(true);
  const [showVolumeMa, setShowVolumeMa] = useState(false);
  const [showBollinger, setShowBollinger] = useState(false);
  const [activePanels, setActivePanels] = useState<string[]>([]);
  const [candles, setCandles] = useState<Candle[]>([]);
  const [hover, setHover] = useState<{ price: number; time: number } | null>(null);
  const tickers = useMarketStore((s) => s.tickers);

  const rawCandles = useMemo(() => candles, [candles]);
  const displayCandles = useMemo(() => (chartType === 'heikin' ? heikinAshi(rawCandles) : rawCandles), [rawCandles, chartType]);

  // Main chart initialization
  useEffect(() => {
    if (!chartContainerRef.current) return;

    const chart = createChart(chartContainerRef.current, {
      layout: { background: { color: 'transparent' }, textColor: '#d4dbe3' },
      grid: { vertLines: { color: '#1e242c' }, horzLines: { color: '#1e242c' } },
      crosshair: { mode: CrosshairMode.Magnet },
      rightPriceScale: { borderColor: '#2a313c', scaleMargins: { top: 0.1, bottom: 0.2 } },
      timeScale: { borderColor: '#2a313c', timeVisible: true, secondsVisible: false },
      watermark: { visible: true, text: `NEXA ${pair}`, fontSize: 28, color: 'rgba(212, 219, 227, 0.06)', vertAlign: 'center', horzAlign: 'center' },
    });
    chartRef.current = chart;

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth, height: chartContainerRef.current.clientHeight });
      }
    };
    window.addEventListener('resize', handleResize);
    handleResize();

    chart.subscribeCrosshairMove((param) => {
      if (param.time && param.point && param.point.y >= 0) {
        const series = mainSeriesRef.current;
        if (series) {
          const price = param.seriesData.get(series) as { value?: number; close?: number } | undefined;
          setHover({ price: price?.close ?? price?.value ?? 0, time: param.time as number });
        }
      } else {
        setHover(null);
      }
    });

    return () => {
      window.removeEventListener('resize', handleResize);
      chart.remove();
    };
  }, [pair]);

  // Sub-panel initialization
  useEffect(() => {
    SUB_PANELS.forEach((p) => {
      const existing = subRefs.current[p.id];
      if (activePanels.includes(p.id)) {
        if (existing?.chart) return;
        const container = document.createElement('div');
        container.style.height = `${p.height}px`;
        container.className = 'border-t border-nexa-700';
        const wrapper = document.getElementById('sub-panels');
        if (wrapper) wrapper.appendChild(container);

        const subChart = createChart(container, {
          layout: { background: { color: 'transparent' }, textColor: '#d4dbe3' },
          grid: { vertLines: { color: '#1e242c' }, horzLines: { color: '#1e242c' } },
          crosshair: { mode: CrosshairMode.Magnet },
          rightPriceScale: { borderColor: '#2a313c' },
          timeScale: { borderColor: '#2a313c', visible: false },
          height: p.height,
        });

        const series: ISeriesApi<'Line'>[] = [];
        let histogram: ISeriesApi<'Histogram'> | null = null;

        if (p.id === 'rsi') {
          series.push(subChart.addLineSeries({ color: '#f59e0b', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'RSI' }));
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: 70 }, { time: 9999999999 as Time, value: 70 },
          ]);
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: 30 }, { time: 9999999999 as Time, value: 30 },
          ]);
        } else if (p.id === 'macd') {
          series.push(subChart.addLineSeries({ color: '#3b82f6', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'MACD' }));
          series.push(subChart.addLineSeries({ color: '#f59e0b', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'Signal' }));
          histogram = subChart.addHistogramSeries({ color: '#22c55e', priceLineVisible: false, priceScaleId: 'left' });
          histogram.priceScale().applyOptions({ scaleMargins: { top: 0.1, bottom: 0.1 } });
        } else if (p.id === 'kdj') {
          series.push(subChart.addLineSeries({ color: '#3b82f6', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'K' }));
          series.push(subChart.addLineSeries({ color: '#f59e0b', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'D' }));
          series.push(subChart.addLineSeries({ color: '#ef4444', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'J' }));
        } else if (p.id === 'stochastic') {
          series.push(subChart.addLineSeries({ color: '#3b82f6', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'K' }));
          series.push(subChart.addLineSeries({ color: '#f59e0b', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'D' }));
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: 80 }, { time: 9999999999 as Time, value: 80 },
          ]);
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: 20 }, { time: 9999999999 as Time, value: 20 },
          ]);
        } else if (p.id === 'atr') {
          series.push(subChart.addLineSeries({ color: '#a855f7', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'ATR' }));
        } else if (p.id === 'williamsR') {
          series.push(subChart.addLineSeries({ color: '#06b6d4', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'W%R' }));
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: -20 }, { time: 9999999999 as Time, value: -20 },
          ]);
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: -80 }, { time: 9999999999 as Time, value: -80 },
          ]);
        } else if (p.id === 'cci') {
          series.push(subChart.addLineSeries({ color: '#ec4899', lineWidth: 1, priceLineVisible: false, lastValueVisible: true, title: 'CCI' }));
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: 100 }, { time: 9999999999 as Time, value: 100 },
          ]);
          subChart.addLineSeries({ color: '#374151', lineWidth: 1, priceLineVisible: false, lastValueVisible: false }).setData([
            { time: 0 as Time, value: -100 }, { time: 9999999999 as Time, value: -100 },
          ]);
        }

        subRefs.current[p.id] = { container, chart: subChart, series, histogram };
      } else if (existing) {
        existing.chart?.remove();
        existing.container?.remove();
        delete subRefs.current[p.id];
      }
    });

    const handleResize = () => {
      SUB_PANELS.forEach((p) => {
        const ref = subRefs.current[p.id];
        if (ref?.chart && ref.container) {
          ref.chart.applyOptions({ width: ref.container.clientWidth, height: p.height });
        }
      });
    };
    window.addEventListener('resize', handleResize);
    handleResize();
    return () => window.removeEventListener('resize', handleResize);
  }, [activePanels]);

  useEffect(() => {
    getCandles(pair, interval).then((data) => {
      const mapped: Candle[] = data.map((c) => ({
        time: Math.floor(c.timestamp / 1e6 / 1000),
        open: parseFloat(c.open),
        high: parseFloat(c.high),
        low: parseFloat(c.low),
        close: parseFloat(c.close),
        volume: parseFloat(c.volume || '0'),
      }));
      setCandles(mapped);
    });
  }, [pair, interval]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    indicatorSeriesRef.current.forEach((s) => chart.removeSeries(s));
    bollingerSeriesRef.current.forEach((s) => chart.removeSeries(s));
    if (volumeSeriesRef.current) chart.removeSeries(volumeSeriesRef.current);
    if (volumeMaSeriesRef.current) chart.removeSeries(volumeMaSeriesRef.current);
    if (mainSeriesRef.current) chart.removeSeries(mainSeriesRef.current);
    indicatorSeriesRef.current = [];
    bollingerSeriesRef.current = [];
    volumeSeriesRef.current = null;
    volumeMaSeriesRef.current = null;
    mainSeriesRef.current = null;

    if (chartType === 'line') {
      const series = chart.addLineSeries({ color: '#3b82f6', lineWidth: 2, priceLineVisible: false });
      series.setData(displayCandles.map((c) => ({ time: c.time as Time, value: c.close })) as LineData<Time>[]);
      mainSeriesRef.current = series;
    } else if (chartType === 'area') {
      const series = chart.addAreaSeries({
        lineColor: '#3b82f6',
        topColor: 'rgba(59, 130, 246, 0.4)',
        bottomColor: 'rgba(59, 130, 246, 0.02)',
        priceLineVisible: false,
      });
      series.setData(displayCandles.map((c) => ({ time: c.time as Time, value: c.close })) as AreaData<Time>[]);
      mainSeriesRef.current = series;
    } else if (chartType === 'bar') {
      const series = chart.addBarSeries({
        upColor: '#0ecb81', downColor: '#f6465d',
        thinBars: false,
      });
      series.setData(displayCandles.map((c) => ({
        time: c.time as Time, open: c.open, high: c.high, low: c.low, close: c.close,
      })) as BarData<Time>[]);
      mainSeriesRef.current = series;
    } else {
      const series = chart.addCandlestickSeries({
        upColor: '#0ecb81', downColor: '#f6465d',
        borderUpColor: '#0ecb81', borderDownColor: '#f6465d',
        wickUpColor: '#0ecb81', wickDownColor: '#f6465d',
      });
      series.setData(displayCandles.map((c) => ({
        time: c.time as Time, open: c.open, high: c.high, low: c.low, close: c.close,
      })) as CandlestickData<Time>[]);
      mainSeriesRef.current = series;
    }

    if (showVolume) {
      const volSeries = chart.addHistogramSeries({
        color: '#3b82f6',
        priceFormat: { type: 'volume' },
        priceScaleId: '',
        priceLineVisible: false,
      });
      volSeries.priceScale().applyOptions({ scaleMargins: { top: 0.85, bottom: 0 }, mode: PriceScaleMode.Percentage });
      volSeries.setData(displayCandles.map((c) => ({
        time: c.time as Time,
        value: c.volume,
        color: c.close >= c.open ? 'rgba(14, 203, 129, 0.5)' : 'rgba(246, 70, 93, 0.5)',
      })) as HistogramData<Time>[]);
      volumeSeriesRef.current = volSeries;

      if (showVolumeMa) {
        const vma = volumeSma(rawCandles, 20);
        const vmaSeries = chart.addLineSeries({ color: '#f59e0b', lineWidth: 1, priceLineVisible: false, lastValueVisible: false });
        vmaSeries.priceScale().applyOptions({ scaleMargins: { top: 0.85, bottom: 0 } });
        vmaSeries.setData(vma.map((p) => ({ time: p.time as Time, value: p.value })) as LineData<Time>[]);
        volumeMaSeriesRef.current = vmaSeries;
      }
    }

    activeIndicators.forEach((id) => {
      const ind = MAIN_INDICATORS.find((i) => i.id === id);
      if (!ind) return;
      const points = ind.fn(rawCandles);
      const series = chart.addLineSeries({ color: ind.color, lineWidth: 1, priceLineVisible: false, lastValueVisible: false });
      series.setData(points.map((p) => ({ time: p.time as Time, value: p.value })) as LineData<Time>[]);
      indicatorSeriesRef.current.push(series);
    });

    if (showBollinger) {
      const bb = bollinger(rawCandles);
      const upper = chart.addLineSeries({ color: '#a855f7', lineWidth: 1, priceLineVisible: false, lastValueVisible: false, lineStyle: LineStyle.Dashed });
      const middle = chart.addLineSeries({ color: '#d4dbe3', lineWidth: 1, priceLineVisible: false, lastValueVisible: false });
      const lower = chart.addLineSeries({ color: '#a855f7', lineWidth: 1, priceLineVisible: false, lastValueVisible: false, lineStyle: LineStyle.Dashed });
      upper.setData(bb.map((b) => ({ time: b.time as Time, value: b.upper })) as LineData<Time>[]);
      middle.setData(bb.map((b) => ({ time: b.time as Time, value: b.middle })) as LineData<Time>[]);
      lower.setData(bb.map((b) => ({ time: b.time as Time, value: b.lower })) as LineData<Time>[]);
      bollingerSeriesRef.current = [upper, middle, lower];
    }

    // Update sub-panels
    if (activePanels.includes('rsi') && subRefs.current.rsi) {
      const data = rsi(rawCandles);
      subRefs.current.rsi.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.value })) as LineData<Time>[]);
      subRefs.current.rsi.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('macd') && subRefs.current.macd) {
      const data = macd(rawCandles);
      subRefs.current.macd.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.macd })) as LineData<Time>[]);
      subRefs.current.macd.series[1].setData(data.map((r) => ({ time: r.time as Time, value: r.signal })) as LineData<Time>[]);
      subRefs.current.macd.histogram?.setData(data.map((r) => ({
        time: r.time as Time,
        value: r.histogram,
        color: r.histogram >= 0 ? 'rgba(14, 203, 129, 0.7)' : 'rgba(246, 70, 93, 0.7)',
      })) as HistogramData<Time>[]);
      subRefs.current.macd.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('kdj') && subRefs.current.kdj) {
      const data = kdj(rawCandles);
      subRefs.current.kdj.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.k })) as LineData<Time>[]);
      subRefs.current.kdj.series[1].setData(data.map((r) => ({ time: r.time as Time, value: r.k })) as LineData<Time>[]);
      subRefs.current.kdj.series[2].setData(data.map((r) => ({ time: r.time as Time, value: r.j })) as LineData<Time>[]);
      subRefs.current.kdj.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('stochastic') && subRefs.current.stochastic) {
      const data = stochastic(rawCandles);
      subRefs.current.stochastic.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.k })) as LineData<Time>[]);
      subRefs.current.stochastic.series[1].setData(data.map((r) => ({ time: r.time as Time, value: r.d })) as LineData<Time>[]);
      subRefs.current.stochastic.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('atr') && subRefs.current.atr) {
      const data = atr(rawCandles);
      subRefs.current.atr.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.value })) as LineData<Time>[]);
      subRefs.current.atr.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('williamsR') && subRefs.current.williamsR) {
      const data = williamsR(rawCandles);
      subRefs.current.williamsR.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.value })) as LineData<Time>[]);
      subRefs.current.williamsR.chart?.timeScale().fitContent();
    }
    if (activePanels.includes('cci') && subRefs.current.cci) {
      const data = cci(rawCandles);
      subRefs.current.cci.series[0].setData(data.map((r) => ({ time: r.time as Time, value: r.value })) as LineData<Time>[]);
      subRefs.current.cci.chart?.timeScale().fitContent();
    }

    chart.timeScale().fitContent();
  }, [displayCandles, rawCandles, chartType, activeIndicators, showVolume, showVolumeMa, showBollinger, activePanels]);

  const toggleIndicator = (id: string) => {
    setActiveIndicators((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  };

  const togglePanel = (id: string) => {
    setActivePanels((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  };

  const lastPrice = tickers[pair]?.last;

  return (
    <Card className="flex h-full flex-col" title={
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>{pair} <span className="text-nexa-400">{formatPrice(lastPrice, 2)}</span></span>
        <div className="flex flex-wrap items-center gap-2">
          {(['candle', 'bar', 'line', 'area', 'heikin'] as ChartType[]).map((t) => (
            <Button key={t} size="sm" variant={chartType === t ? 'primary' : 'ghost'} onClick={() => setChartType(t)}>
              {t[0].toUpperCase() + t.slice(1)}
            </Button>
          ))}
          <Select className="w-20" value={interval} onChange={(e) => setInterval(e.target.value)} options={INTERVALS.map((i) => ({ value: i, label: i }))} />
        </div>
      </div>
    }>
      <div className="flex flex-wrap gap-2 px-2 py-1 text-xs">
        {MAIN_INDICATORS.map((ind) => (
          <button
            key={ind.id}
            onClick={() => toggleIndicator(ind.id)}
            className={`rounded px-2 py-0.5 border ${activeIndicators.includes(ind.id) ? 'border-accent bg-accent/20 text-accent' : 'border-nexa-700 text-nexa-400 hover:bg-nexa-800'}`}
          >
            <span className="mr-1 inline-block h-2 w-2 rounded-full" style={{ background: ind.color }} />{ind.label}
          </button>
        ))}
        <button onClick={() => setShowBollinger((v) => !v)} className={`rounded px-2 py-0.5 border ${showBollinger ? 'border-accent bg-accent/20 text-accent' : 'border-nexa-700 text-nexa-400 hover:bg-nexa-800'}`}>Bollinger</button>
        <button onClick={() => setShowVolume((v) => !v)} className={`rounded px-2 py-0.5 border ${showVolume ? 'border-accent bg-accent/20 text-accent' : 'border-nexa-700 text-nexa-400 hover:bg-nexa-800'}`}>Volume</button>
        <button onClick={() => setShowVolumeMa((v) => !v)} className={`rounded px-2 py-0.5 border ${showVolumeMa ? 'border-accent bg-accent/20 text-accent' : 'border-nexa-700 text-nexa-400 hover:bg-nexa-800'}`}>VMA20</button>
        {SUB_PANELS.map((p) => (
          <button
            key={p.id}
            onClick={() => togglePanel(p.id)}
            className={`rounded px-2 py-0.5 border ${activePanels.includes(p.id) ? 'border-accent bg-accent/20 text-accent' : 'border-nexa-700 text-nexa-400 hover:bg-nexa-800'}`}
          >
            {p.label}
          </button>
        ))}
      </div>
      <div className="relative flex-1 min-h-[300px]">
        <div ref={chartContainerRef} className="absolute inset-0" style={{ bottom: activePanels.length > 0 ? undefined : 0 }} />
        {hover && (
          <div className="pointer-events-none absolute right-2 top-2 z-10 rounded bg-nexa-900/90 px-2 py-1 text-xs font-mono text-nexa-100 shadow">
            <div>O: {formatPrice(hover.price, 2)}</div>
          </div>
        )}
      </div>
      <div id="sub-panels" className="w-full" />
    </Card>
  );
}
