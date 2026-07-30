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

export interface MACDPoint {
  time: number;
  macd: number;
  signal: number;
  histogram: number;
}

export function macd(data: Candle[], fast = 12, slow = 26, signal = 9): MACDPoint[] {
  if (data.length < slow + signal) return [];
  const emaFast = ema(data, fast).map((p) => p.value);
  const emaSlow = ema(data, slow).map((p) => p.value);
  const macdLine: number[] = [];
  for (let i = 0; i < data.length; i++) {
    macdLine.push(emaFast[i] - emaSlow[i]);
  }
  // Signal = EMA of MACD line
  const signalLine: number[] = [];
  let sigPrev = macdLine[0];
  const k = 2 / (signal + 1);
  for (let i = 0; i < macdLine.length; i++) {
    if (i === 0) {
      signalLine.push(sigPrev);
    } else {
      sigPrev = macdLine[i] * k + sigPrev * (1 - k);
      signalLine.push(sigPrev);
    }
  }
  const out: MACDPoint[] = [];
  for (let i = slow + signal - 2; i < data.length; i++) {
    out.push({
      time: data[i].time,
      macd: macdLine[i],
      signal: signalLine[i],
      histogram: macdLine[i] - signalLine[i],
    });
  }
  return out;
}

export interface KDJPoint {
  time: number;
  k: number;
  d: number;
  j: number;
}

export function kdj(data: Candle[], n = 9, m1 = 3, m2 = 3): KDJPoint[] {
  if (data.length < n) return [];
  const out: KDJPoint[] = [];
  let prevK = 50;
  let prevD = 50;
  for (let i = n - 1; i < data.length; i++) {
    let low = data[i].low;
    let high = data[i].high;
    for (let j = 0; j < n; j++) {
      low = Math.min(low, data[i - j].low);
      high = Math.max(high, data[i - j].high);
    }
    const rsv = high === low ? 0 : ((data[i].close - low) / (high - low)) * 100;
    const k = (2 * prevK + rsv) / (m1 + 1);
    const d = (2 * prevD + k) / (m2 + 1);
    const j = 3 * k - 2 * d;
    out.push({ time: data[i].time, k, d, j });
    prevK = k;
    prevD = d;
  }
  return out;
}

export interface StochasticPoint {
  time: number;
  k: number;
  d: number;
}

export function stochastic(data: Candle[], kPeriod = 14, dPeriod = 3): StochasticPoint[] {
  if (data.length < kPeriod + dPeriod - 1) return [];
  const rawK: number[] = [];
  for (let i = kPeriod - 1; i < data.length; i++) {
    let low = data[i].low;
    let high = data[i].high;
    for (let j = 0; j < kPeriod; j++) {
      low = Math.min(low, data[i - j].low);
      high = Math.max(high, data[i - j].high);
    }
    rawK.push(high === low ? 50 : ((data[i].close - low) / (high - low)) * 100);
  }
  const out: StochasticPoint[] = [];
  for (let i = dPeriod - 1; i < rawK.length; i++) {
    let sumK = 0;
    for (let j = 0; j < dPeriod; j++) sumK += rawK[i - j];
    const k = sumK / dPeriod;
    let sumD = 0;
    for (let j = 0; j < dPeriod; j++) {
      let s = 0;
      for (let x = 0; x < dPeriod; x++) s += rawK[i - j - x];
      sumD += s / dPeriod;
    }
    const d = sumD / dPeriod;
    out.push({ time: data[i + kPeriod - 1].time, k, d });
  }
  return out;
}

export function vwap(data: Candle[]): LinePoint[] {
  if (data.length === 0) return [];
  const out: LinePoint[] = [];
  let cumTPV = 0;
  let cumVol = 0;
  for (const c of data) {
    const tp = (c.high + c.low + c.close) / 3;
    cumTPV += tp * c.volume;
    cumVol += c.volume;
    out.push({ time: c.time, value: cumVol === 0 ? tp : cumTPV / cumVol });
  }
  return out;
}

export function volumeSma(data: Candle[], period = 20): LinePoint[] {
  if (period <= 0 || data.length < period) return [];
  const out: LinePoint[] = [];
  let sum = 0;
  for (let i = 0; i < data.length; i++) {
    sum += data[i].volume;
    if (i >= period) sum -= data[i - period].volume;
    if (i >= period - 1) {
      out.push({ time: data[i].time, value: sum / period });
    }
  }
  return out;
}

export function atr(data: Candle[], period = 14): LinePoint[] {
  if (period <= 0 || data.length < 2) return [];
  const tr: number[] = [];
  for (let i = 0; i < data.length; i++) {
    const hL = data[i].high - data[i].low;
    if (i === 0) {
      tr.push(hL);
    } else {
      const hPc = Math.abs(data[i].high - data[i - 1].close);
      const lPc = Math.abs(data[i].low - data[i - 1].close);
      tr.push(Math.max(hL, hPc, lPc));
    }
  }
  const out: LinePoint[] = [];
  let prev = 0;
  for (let i = 0; i < tr.length; i++) {
    if (i < period - 1) continue;
    if (i === period - 1) {
      let sum = 0;
      for (let j = 0; j < period; j++) sum += tr[j];
      prev = sum / period;
    } else {
      prev = (prev * (period - 1) + tr[i]) / period;
    }
    out.push({ time: data[i].time, value: prev });
  }
  return out;
}

export function williamsR(data: Candle[], period = 14): LinePoint[] {
  if (period <= 0 || data.length < period) return [];
  const out: LinePoint[] = [];
  for (let i = period - 1; i < data.length; i++) {
    let high = data[i].high;
    let low = data[i].low;
    for (let j = 0; j < period; j++) {
      high = Math.max(high, data[i - j].high);
      low = Math.min(low, data[i - j].low);
    }
    const value = high === low ? -50 : ((high - data[i].close) / (high - low)) * -100;
    out.push({ time: data[i].time, value });
  }
  return out;
}

export function cci(data: Candle[], period = 20): LinePoint[] {
  if (period <= 0 || data.length < period) return [];
  const out: LinePoint[] = [];
  for (let i = period - 1; i < data.length; i++) {
    let sum = 0;
    for (let j = 0; j < period; j++) {
      sum += (data[i - j].high + data[i - j].low + data[i - j].close) / 3;
    }
    const sma = sum / period;
    let md = 0;
    for (let j = 0; j < period; j++) {
      md += Math.abs((data[i - j].high + data[i - j].low + data[i - j].close) / 3 - sma);
    }
    md /= period;
    const tp = (data[i].high + data[i].low + data[i].close) / 3;
    const value = md === 0 ? 0 : (tp - sma) / (0.015 * md);
    out.push({ time: data[i].time, value });
  }
  return out;
}

export interface SuperTrendPoint {
  time: number;
  value: number;
  direction: 1 | -1;
}

export function superTrend(data: Candle[], period = 10, multiplier = 3): SuperTrendPoint[] {
  if (period <= 0 || data.length <= period) return [];
  const atrData = atr(data, period);
  const out: SuperTrendPoint[] = [];
  let prevFinalUpper = 0;
  let prevFinalLower = 0;
  let prevClose = 0;
  let direction: 1 | -1 = 1;

  for (let i = period - 1; i < data.length; i++) {
    const c = data[i];
    const atrValue = atrData[i - (period - 1)]?.value ?? c.high - c.low;
    const median = (c.high + c.low) / 2;
    const upper = median + multiplier * atrValue;
    const lower = median - multiplier * atrValue;

    if (i === period - 1) {
      prevFinalUpper = upper;
      prevFinalLower = lower;
      prevClose = c.close;
      direction = c.close > upper ? 1 : -1;
      out.push({ time: c.time, value: direction === 1 ? lower : upper, direction });
      continue;
    }

    const finalUpper = upper < prevFinalUpper || prevClose > prevFinalUpper ? upper : prevFinalUpper;
    const finalLower = lower > prevFinalLower || prevClose < prevFinalLower ? lower : prevFinalLower;

    let newDirection: 1 | -1 = direction;
    if (c.close > prevFinalUpper) newDirection = 1;
    else if (c.close < prevFinalLower) newDirection = -1;

    out.push({ time: c.time, value: newDirection === 1 ? finalLower : finalUpper, direction: newDirection });
    prevFinalUpper = finalUpper;
    prevFinalLower = finalLower;
    prevClose = c.close;
    direction = newDirection;
  }
  return out;
}

export interface IchimokuPoint {
  time: number;
  tenkan: number;
  kijun: number;
  senkouA: number;
  senkouB: number;
  chikou: number;
}

function donchianMid(data: Candle[], end: number, period: number): number {
  let high = -Infinity;
  let low = Infinity;
  const start = Math.max(0, end - period + 1);
  for (let i = start; i <= end; i++) {
    high = Math.max(high, data[i].high);
    low = Math.min(low, data[i].low);
  }
  return high === -Infinity ? data[end].close : (high + low) / 2;
}

export function ichimoku(
  data: Candle[],
  tenkan = 9,
  kijun = 26,
  senkouB = 52,
  displacement = 26,
): IchimokuPoint[] {
  if (data.length < senkouB) return [];
  const out: IchimokuPoint[] = [];
  for (let i = senkouB - 1; i < data.length; i++) {
    const t = donchianMid(data, i, tenkan);
    const k = donchianMid(data, i, kijun);
    const sb = donchianMid(data, i, senkouB);
    const sa = (t + k) / 2;
    const chikou = i >= displacement ? data[i - displacement].close : data[i].close;
    out.push({ time: data[i].time, tenkan: t, kijun: k, senkouA: sa, senkouB: sb, chikou });
  }
  return out;
}

export interface FibonacciLevel {
  level: number;
  price: number;
}

export function fibonacciLevels(high: number, low: number): FibonacciLevel[] {
  const diff = high - low;
  const levels = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1];
  return levels.map((level) => ({ level, price: high - diff * level }));
}

export interface PivotPoint {
  time: number;
  pp: number;
  r1: number;
  r2: number;
  r3: number;
  s1: number;
  s2: number;
  s3: number;
}

export function pivotPoints(data: Candle[]): PivotPoint[] {
  if (data.length < 2) return [];
  const out: PivotPoint[] = [];
  for (let i = 1; i < data.length; i++) {
    const prev = data[i - 1];
    const pp = (prev.high + prev.low + prev.close) / 3;
    const r1 = 2 * pp - prev.low;
    const s1 = 2 * pp - prev.high;
    const r2 = pp + prev.high - prev.low;
    const s2 = pp - r1 + s1;
    const r3 = prev.high + 2 * (pp - prev.low);
    const s3 = prev.low - 2 * (prev.high - pp);
    out.push({ time: data[i].time, pp, r1, r2, r3, s1, s2, s3 });
  }
  return out;
}

export function obv(data: Candle[]): LinePoint[] {
  if (data.length === 0) return [];
  const out: LinePoint[] = [];
  let value = 0;
  for (let i = 0; i < data.length; i++) {
    if (i > 0) {
      const prev = data[i - 1];
      if (data[i].close > prev.close) value += data[i].volume;
      else if (data[i].close < prev.close) value -= data[i].volume;
    }
    out.push({ time: data[i].time, value });
  }
  return out;
}

export function mfi(data: Candle[], period = 14): LinePoint[] {
  if (period <= 0 || data.length <= period) return [];
  const tp = data.map((c) => (c.high + c.low + c.close) / 3);
  const out: LinePoint[] = [];
  for (let i = period; i < data.length; i++) {
    let pos = 0;
    let neg = 0;
    for (let j = i - period + 1; j <= i; j++) {
      const moneyFlow = tp[j] * data[j].volume;
      if (tp[j] > tp[j - 1]) pos += moneyFlow;
      else if (tp[j] < tp[j - 1]) neg += moneyFlow;
    }
    const ratio = neg === 0 ? Infinity : pos / neg;
    const value = ratio === Infinity ? 100 : 100 - 100 / (1 + ratio);
    out.push({ time: data[i].time, value });
  }
  return out;
}
