import { cls } from '@/utils/format';

export function Badge({ children, color = 'neutral' }: { children: React.ReactNode; color?: 'neutral' | 'up' | 'down' | 'accent' }) {
  const map = {
    neutral: 'bg-nexa-700 text-nexa-100',
    up: 'bg-up/15 text-up',
    down: 'bg-down/15 text-down',
    accent: 'bg-accent/15 text-accent',
  };
  return (
    <span className={cls('inline-flex items-center rounded px-2 py-0.5 text-xs font-medium', map[color])}>
      {children}
    </span>
  );
}
