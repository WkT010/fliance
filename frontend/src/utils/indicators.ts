export interface Candle {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface LinePoint {
  time: number;
  value: number;
}

export function sma(data: Candle[], period: number): LinePoint[] {
  if (period <= 0 || data.length < period) return [];
  const out: LinePoint[] = [];
  let sum = 0;
  for (let i = 0; i < data.length; i++) {
    sum += data[i].close;
    if (i >= period) sum -= data[i - period].close;
    if (i >= period - 1) {
      out.push({ time: data[i].time, value: sum / period });
    }
  }
  return out;
}

export function ema(data: Candle[], period: number): LinePoint[] {
  if (period <= 0 || data.length === 0) return [];
  const k = 2 / (period + 1);
  const out: LinePoint[] = [];
  let prev = data[0].close;
  for (let i = 0; i < data.length; i++) {
    if (i === 0) {
      out.push({ time: data[i].time, value: prev });
      continue;
    }
    prev = data[i].close * k + prev * (1 - k);
    out.push({ time: data[i].time, value: prev });
  }
  return out;
}

export function rsi(data: Candle[], period = 14): LinePoint[] {
  if (period <= 0 || data.length <= period) return [];
  const out: LinePoint[] = [];
  let gain = 0;
  let loss = 0;
  for (let i = 1; i <= period; i++) {
    const diff = data[i].close - data[i - 1].close;
    if (diff > 0) gain += diff;
    else loss -= diff;
  }
  let avgGain = gain / period;
  let avgLoss = loss / period;
  for (let i = period; i < data.length; i++) {
    const diff = data[i].close - data[i - 1].close;
    if (diff > 0) {
      avgGain = (avgGain * (period - 1) + diff) / period;
      avgLoss = (avgLoss * (period - 1)) / period;
    } else {
      avgGain = (avgGain * (period - 1)) / period;
      avgLoss = (avgLoss * (period - 1) - diff) / period;
    }
    const rs = avgLoss === 0 ? 100 : avgGain / avgLoss;
    const value = avgLoss === 0 ? 100 : 100 - 100 / (1 + rs);
    out.push({ time: data[i].time, value });
  }
  return out;
}

export interface BollingerPoint {
  time: number;
  upper: number;
  middle: number;
  lower: number;
}

export function bollinger(data: Candle[], period = 20, mult = 2): BollingerPoint[] {
  if (period <= 0 || data.length < period) return [];
  const out: BollingerPoint[] = [];
  for (let i = period - 1; i < data.length; i++) {
    let sum = 0;
    for (let j = 0; j < period; j++) sum += data[i - j].close;
    const middle = sum / period;
    let sq = 0;
    for (let j = 0; j < period; j++) sq += Math.pow(data[i - j].close - middle, 2);
    const std = Math.sqrt(sq / period);
    out.push({ time: data[i].time, upper: middle + mult * std, middle, lower: middle - mult * std });
  }
  return out;
}

export function heikinAshi(data: Candle[]): Candle[] {
  if (data.length === 0) return [];
  const out: Candle[] = [];
  let prevHA: Candle | null = null;
  for (const c of data) {
    const close = (c.open + c.high + c.low + c.close) / 4;
    const open = prevHA ? (prevHA.open + prevHA.close) / 2 : (c.open + c.close) / 2;
    const high = Math.max(c.high, open, close);
    const low = Math.min(c.low, open, close);
    const ha: Candle = { time: c.time, open, high, low, close, volume: c.volume };
    out.push(ha);
    prevHA = ha;
  }
  return out;
}
