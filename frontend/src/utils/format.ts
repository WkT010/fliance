/**
 * Format helpers used across the UI.
 *
 *  - formatPrice  : fixed-decimal display, e.g. "12,345.67".  Handles very
 *                   large or very small values without falling into scientific
 *                   notation by switching to a smart-precision mode outside
 *                   a sensible "normal" range.
 *  - formatQty    : shorter variant for quantities (defaults to 6 dp).
 *  - formatUsd    : always shows 2 dp, with a $ sign and K/M/B suffix for
 *                   large values so a $10M balance is readable.
 *  - formatTime   : HH:MM:SS in 24-hour clock; auto-detects ms/µs/ns input.
 *  - formatDate   : short locale date; auto-detects ms/µs/ns input.
 *  - formatPct    : percent with sign, used for 24h change etc.
 *  - changeColor  : green / red / neutral, 0 → neutral.
 *  - shortAddr    : collapses "0x1234…abcd" for display in tables.
 *  - cls          : tiny className joiner.
 */

const SCIENTIFIC_THRESHOLD = 1e-9;     // below this → use scientific precision
const NORMAL_UPPER = 1e15;             // above this → use smart precision

function smartPrecision(n: number, fallback: number): number {
  if (!Number.isFinite(n) || n === 0) return fallback;
  const abs = Math.abs(n);
  if (abs < SCIENTIFIC_THRESHOLD) return 8;
  if (abs >= NORMAL_UPPER) return 0;
  if (abs >= 1) return Math.min(fallback, 4);
  if (abs >= 1e-4) return Math.max(fallback, 4);
  if (abs >= 1e-6) return 6;
  return 8;
}

function fmt(n: number, decimals: number): string {
  if (!Number.isFinite(n)) return '--';
  return n.toLocaleString('en-US', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
}

export function formatPrice(
  value: string | number | undefined | null,
  decimals = 2
): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return fmt(n, smartPrecision(n, decimals));
}

export function formatQty(
  value: string | number | undefined | null,
  decimals = 6
): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return fmt(n, smartPrecision(n, decimals));
}

/** Format an amount as USD: 12,345,678.90 / 1.23M / 4.56K / 0.0012 */
export function formatUsd(value: string | number | undefined | null, decimals = 2): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n) || !Number.isFinite(n)) return '--';
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  if (abs >= 1e9) return `${sign}$${(abs / 1e9).toFixed(decimals)}B`;
  if (abs >= 1e6) return `${sign}$${(abs / 1e6).toFixed(decimals)}M`;
  if (abs >= 1e3) return `${sign}$${(abs / 1e3).toFixed(decimals)}K`;
  if (abs < 1e-4 && abs > 0) return `${sign}$${abs.toExponential(2)}`;
  return `${sign}$${fmt(abs, decimals)}`;
}

/**
 * Normalise a backend timestamp to milliseconds. Backends mix units (ms for
 * some endpoints, µs and ns for others), so detect by magnitude instead of
 * assuming one unit: ms values stay below 1e13 until ~2286, µs below 1e16
 * for millennia, everything larger is ns.
 */
function toMillis(t: number): number {
  if (t < 1e13) return t;        // already milliseconds
  if (t < 1e16) return t / 1e3;  // microseconds
  return t / 1e6;                // nanoseconds
}

export function formatTime(ts: number | string | undefined | null): string {
  if (ts === undefined || ts === null) return '--';
  const t = typeof ts === 'string' ? parseInt(ts, 10) : ts;
  if (Number.isNaN(t)) return '--';
  const d = new Date(toMillis(t));
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function formatDate(ts: number | string | undefined | null): string {
  if (ts === undefined || ts === null) return '--';
  const t = typeof ts === 'string' ? parseInt(ts, 10) : ts;
  if (Number.isNaN(t)) return '--';
  const d = new Date(toMillis(t));
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: '2-digit' });
}

export function formatPct(value: string | number | undefined | null, decimals = 2): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return `${n > 0 ? '+' : ''}${n.toFixed(decimals)}%`;
}

/**
 * formatChangePct: the backend sends change_pct_24h as a decimal fraction
 * (e.g. 0.0022 for +0.22%), so scale to percentage points before formatting.
 * Use this for change_pct_24h only; values already expressed in percent
 * (e.g. PnL %, price-diff %) must keep using formatPct directly.
 */
export function formatChangePct(value: string | number | undefined | null, decimals = 2): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return formatPct(n * 100, decimals);
}

export function changeColorClass(value: string | number | undefined | null): string {
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (n === undefined || n === null || Number.isNaN(n) || n === 0) return 'text-nexa-300';
  return n > 0 ? 'text-up' : 'text-down';
}

export function shortAddr(addr: string | undefined | null, head = 6, tail = 4): string {
  if (!addr) return '--';
  if (addr.length <= head + tail + 1) return addr;
  return `${addr.slice(0, head)}…${addr.slice(-tail)}`;
}

export function cls(...classes: (string | false | undefined | null)[]): string {
  return classes.filter(Boolean).join(' ');
}
