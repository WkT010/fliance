import { cls } from '@/utils/format';

interface CardProps {
  children: React.ReactNode;
  className?: string;
  title?: React.ReactNode;
  /** When true, the card lifts on hover (use for clickable cards). */
  interactive?: boolean;
  /** Optional extra content rendered to the right of the title. */
  extra?: React.ReactNode;
  /** Reduce inner padding. */
  flush?: boolean;
}

export function Card({ children, className, title, interactive, extra, flush }: CardProps) {
  return (
    <div
      className={cls(
        'group relative overflow-hidden rounded-xl border border-nexa-700/70 bg-nexa-800/60 shadow-lg shadow-black/20',
        'transition-all duration-200',
        interactive && 'cursor-pointer hover:border-nexa-600 hover:shadow-xl hover:shadow-black/30 hover:-translate-y-0.5',
        className
      )}
    >
      {title && (
        <div className="flex items-center justify-between border-b border-nexa-700/70 bg-nexa-900/40 px-4 py-2.5">
          <div className="text-sm font-semibold tracking-wide text-nexa-100">{title}</div>
          {extra && <div className="text-xs text-nexa-400">{extra}</div>}
        </div>
      )}
      <div className={cls(flush ? '' : 'p-4')}>{children}</div>
    </div>
  );
}
