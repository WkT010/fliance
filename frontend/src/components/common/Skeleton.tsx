import { cls } from '@/utils/format';

interface SkeletonProps {
  className?: string;
  /** Number of times to render the line. */
  count?: number;
  /** Gap between lines in tailwind units. */
  gap?: 1 | 2 | 3 | 4;
  /** Optional shimmer effect. */
  shimmer?: boolean;
}

export function Skeleton({ className, count = 1, gap = 2, shimmer = true }: SkeletonProps) {
  const items = Array.from({ length: count });
  return (
    <div className={cls('flex flex-col', `gap-${gap}`)}>
      {items.map((_, i) => (
        <div
          key={i}
          className={cls(
            'h-3 w-full rounded-md bg-nexa-800/80',
            shimmer && 'animate-shimmer',
            className
          )}
        />
      ))}
    </div>
  );
}

interface SpinnerProps {
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}

export function Spinner({ size = 'md', className }: SpinnerProps) {
  const sizes = { sm: 'h-4 w-4 border-2', md: 'h-6 w-6 border-2', lg: 'h-10 w-10 border-[3px]' };
  return (
    <span
      className={cls(
        'inline-block animate-spin rounded-full border-current border-t-transparent text-accent',
        sizes[size],
        className
      )}
      role="status"
      aria-label="Loading"
    />
  );
}
