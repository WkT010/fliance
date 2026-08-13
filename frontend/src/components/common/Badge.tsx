import { cls } from '@/utils/format';

type BadgeColor = 'neutral' | 'up' | 'down' | 'accent' | 'danger' | 'success' | 'warning' | 'info';

const map: Record<BadgeColor, string> = {
  neutral: 'bg-nexa-700 text-nexa-100 border border-nexa-600/50',
  up: 'bg-up/10 text-up border border-up/20',
  down: 'bg-down/10 text-down border border-down/20',
  accent: 'bg-accent/10 text-accent border border-accent/20',
  danger: 'bg-down/10 text-down border border-down/20',
  success: 'bg-up/10 text-up border border-up/20',
  warning: 'bg-cta/10 text-cta border border-cta/20',
  info: 'bg-sky-400/10 text-sky-300 border border-sky-400/20',
};

interface BadgeProps {
  children: React.ReactNode;
  color?: BadgeColor;
  className?: string;
  size?: 'sm' | 'md';
}

export function Badge({ children, color = 'neutral', className, size = 'md' }: BadgeProps) {
  return (
    <span
      className={cls(
        'inline-flex items-center gap-1 rounded-md font-medium',
        size === 'sm' ? 'px-1.5 py-0.5 text-[10px]' : 'px-2 py-0.5 text-xs',
        map[color],
        className
      )}
    >
      {children}
    </span>
  );
}
