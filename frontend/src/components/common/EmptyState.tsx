import { cls } from '@/utils/format';

interface EmptyStateProps {
  title: string;
  description?: string;
  icon?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
  compact?: boolean;
}

export function EmptyState({ title, description, icon, action, className, compact }: EmptyStateProps) {
  return (
    <div
      className={cls(
        'flex flex-col items-center justify-center text-center',
        compact ? 'py-6' : 'py-12',
        className
      )}
    >
      {icon ? (
        <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-nexa-800/80 text-nexa-500">
          {icon}
        </div>
      ) : (
        <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-nexa-800/80 text-nexa-500">
          <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
            <path d="M4 7h16M4 12h16M4 17h10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </div>
      )}
      <h3 className="text-sm font-medium text-nexa-200">{title}</h3>
      {description && <p className="mt-1 max-w-sm text-xs text-nexa-500">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
