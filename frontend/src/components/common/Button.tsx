import { cls } from '@/utils/format';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'success' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  isLoading?: boolean;
  block?: boolean;
  /** Adds a soft leading icon. */
  icon?: React.ReactNode;
}

export function Button({
  children,
  variant = 'primary',
  size = 'md',
  isLoading,
  className,
  disabled,
  block,
  icon,
  ...props
}: ButtonProps) {
  const base =
    'relative inline-flex items-center justify-center gap-2 rounded-lg font-medium ' +
    'transition-all duration-150 select-none ' +
    'focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 focus-visible:ring-offset-2 focus-visible:ring-offset-nexa-900 ' +
    'disabled:cursor-not-allowed disabled:opacity-60 active:scale-[0.98]';

  const variants: Record<NonNullable<ButtonProps['variant']>, string> = {
    primary:
      'bg-accent text-nexa-950 shadow-md shadow-accent/20 ' +
      'hover:bg-accent/90 hover:shadow-lg hover:shadow-accent/30',
    secondary:
      'bg-nexa-700 text-nexa-100 border border-nexa-600/60 ' +
      'hover:bg-nexa-600 hover:border-nexa-500',
    ghost:
      'bg-transparent text-nexa-300 ' +
      'hover:bg-nexa-800 hover:text-nexa-100',
    danger:
      'bg-down text-white shadow-md shadow-down/20 ' +
      'hover:bg-down/90 hover:shadow-lg hover:shadow-down/30',
    success:
      'bg-up text-nexa-950 shadow-md shadow-up/20 ' +
      'hover:bg-up/90 hover:shadow-lg hover:shadow-up/30',
    outline:
      'bg-transparent text-nexa-100 border border-nexa-600 ' +
      'hover:bg-nexa-800 hover:border-nexa-500',
  };

  const sizes: Record<NonNullable<ButtonProps['size']>, string> = {
    sm: 'px-3 py-1.5 text-xs',
    md: 'px-4 py-2 text-sm',
    lg: 'px-6 py-2.5 text-base',
  };

  return (
    <button
      className={cls(base, variants[variant], sizes[size], block && 'w-full', className)}
      disabled={disabled || isLoading}
      {...props}
    >
      {isLoading ? (
        <span className="inline-flex items-center">
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
          <span className="ml-2 opacity-80">{children}</span>
        </span>
      ) : (
        <>
          {icon && <span className="flex-shrink-0">{icon}</span>}
          {children}
        </>
      )}
    </button>
  );
}
