import type { Theme } from '@/store/themeStore';

/**
 * Canvas-level palettes for lightweight-charts and hand-drawn canvases
 * (DepthChart). These libraries paint their own pixels, so they cannot
 * consume CSS variables directly — each theme gets an explicit palette.
 * gain/loss are deepened in light mode for contrast on white backgrounds,
 * mirroring the --c-gain / --c-loss CSS tokens.
 */
export interface ChartPalette {
  /** Axis label / scale text */
  text: string;
  /** Grid lines */
  grid: string;
  /** Price/time scale borders */
  border: string;
  /** Center watermark */
  watermark: string;
  /** Semantic up/down colors (series, candles, histograms) */
  gain: string;
  loss: string;
  gainSoft: string;
  lossSoft: string;
  /** Translucent fills for depth-chart areas */
  gainFill: string;
  lossFill: string;
  /** Neutral reference lines (RSI 30/70, stochastic bands…) */
  refLine: string;
  /** Bollinger middle band / pivot PP */
  neutralLine: string;
  /** Depth chart extras */
  axisLabel: string;
  midLine: string;
  mutedLabel: string;
}

export const CHART_PALETTES: Record<Theme, ChartPalette> = {
  dark: {
    text: '#EAECEF',
    grid: '#2B3139',
    border: '#333B47',
    watermark: 'rgba(234, 236, 239, 0.06)',
    gain: '#2EBD85',
    loss: '#F6465D',
    gainSoft: 'rgba(46, 189, 133, 0.5)',
    lossSoft: 'rgba(246, 70, 93, 0.5)',
    gainFill: 'rgba(14, 203, 129, 0.15)',
    lossFill: 'rgba(246, 70, 93, 0.15)',
    refLine: '#374151',
    neutralLine: '#d4dbe3',
    axisLabel: '#5d6b7a',
    midLine: '#2DD4BF',
    mutedLabel: '#8b97a8',
  },
  light: {
    text: '#1B1E24',
    grid: '#E3E7EC',
    border: '#D8DDE4',
    watermark: 'rgba(27, 30, 36, 0.05)',
    gain: '#0A8F5F',
    loss: '#D92E45',
    gainSoft: 'rgba(10, 143, 95, 0.5)',
    lossSoft: 'rgba(217, 46, 69, 0.5)',
    gainFill: 'rgba(10, 143, 95, 0.15)',
    lossFill: 'rgba(217, 46, 69, 0.15)',
    refLine: '#C9CFD8',
    neutralLine: '#5E6675',
    axisLabel: '#8B93A1',
    midLine: '#0D9488',
    mutedLabel: '#5E6675',
  },
};

/** lightweight-charts layout options shared by main + sub charts. */
export function chartLayoutOptions(theme: Theme) {
  const pal = CHART_PALETTES[theme];
  return {
    layout: { background: { color: 'transparent' }, textColor: pal.text },
    grid: { vertLines: { color: pal.grid }, horzLines: { color: pal.grid } },
    rightPriceScale: { borderColor: pal.border },
    timeScale: { borderColor: pal.border },
  };
}
