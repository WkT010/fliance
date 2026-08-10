import { cls, changeColorClass, formatPct } from '@/utils/format';

interface StatCardProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  /** Optional accent color (semantic): up, down, accent, neutral. */
  tone?: 'up' | 'down' | 'accent' | 'neutral';
  /** Optional icon displayed at top-right. */
  icon?: React.ReactNode;
  className?: string;
  /** Optional percent change (positive=up, negative=down) for the hint area. */
  change?: string | number;
  loading?: boolean;
}

const TONE_RING: Record<NonNullable<StatCardProps['tone']>, string> = {
  up: 'from-up/30 to-transparent',
  down: 'from-down/30 to-transparent',
  accent: 'from-accent/30 to-transparent',
  neutral: 'from-nexa-700/40 to-transparent',
};

const TONE_TEXT: Record<NonNullable<StatCardProps['tone']>, string> = {
  up: 'text-up',
  down: 'text-down',
  accent: 'text-accent',
  neutral: 'text-nexa-100',
};

export function StatCard({
  label,
  value,
  hint,
  tone = 'neutral',
  icon,
  className,
  change,
  loading,
}: StatCardProps) {
  return (
    <div
      className={cls(
        'relative overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 p-4 shadow-lg shadow-black/20 transition-all',
        'hover:border-nexa-600/80 hover:shadow-xl',
        className
      )}
    >
      <div
        className={cls(
          'pointer-events-none absolute -right-12 -top-12 h-40 w-40 rounded-full bg-gradient-to-br opacity-40 blur-2xl',
          TONE_RING[tone]
        )}
      />
      <div className="relative flex items-start justify-between">
        <div className="text-xs font-medium uppercase tracking-wide text-nexa-400">{label}</div>
        {icon && <div className="text-nexa-500">{icon}</div>}
      </div>
      <div className={cls('relative mt-2 font-mono text-2xl font-semibold', TONE_TEXT[tone])}>
        {loading ? (
          <span className="inline-block h-7 w-32 animate-shimmer rounded-md bg-nexa-700/50" />
        ) : (
          value
        )}
      </div>
      {(hint || change !== undefined) && (
        <div className="relative mt-1 flex items-center gap-2 text-xs text-nexa-400">
          {change !== undefined && (
            <span className={changeColorClass(change)}>
              {change !== null ? formatPct(change) : '--'}
            </span>
          )}
          {hint && <span>{hint}</span>}
        </div>
      )}
    </div>
  );
}
