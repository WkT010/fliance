export function formatPrice(value: string | number | undefined, decimals = 2): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return n.toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

export function formatQty(value: string | number | undefined, decimals = 6): string {
  if (value === undefined || value === null || value === '') return '--';
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (Number.isNaN(n)) return '--';
  return n.toLocaleString('en-US', { minimumFractionDigits: Math.min(2, decimals), maximumFractionDigits: decimals });
}

export function formatTime(ts: number): string {
  const d = new Date(typeof ts === 'string' ? parseInt(ts, 10) / 1e6 : ts / 1e6);
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function formatDate(ts: number): string {
  const d = new Date(typeof ts === 'string' ? parseInt(ts, 10) / 1e6 : ts / 1e6);
  return d.toLocaleDateString('en-US');
}

export function changeColorClass(value: string | number | undefined): string {
  const n = typeof value === 'string' ? parseFloat(value) : value;
  if (n === undefined || Number.isNaN(n) || n === 0) return 'text-nexa-300';
  return n > 0 ? 'text-up' : 'text-down';
}

export function cls(...classes: (string | false | undefined)[]): string {
  return classes.filter(Boolean).join(' ');
}
