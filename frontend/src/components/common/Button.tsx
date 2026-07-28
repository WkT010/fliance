import { cls } from '@/utils/format';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'success';
  size?: 'sm' | 'md' | 'lg';
  isLoading?: boolean;
}

export function Button({ children, variant = 'primary', size = 'md', isLoading, className, disabled, ...props }: ButtonProps) {
  const base = 'inline-flex items-center justify-center rounded font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-accent/50';
  const variants = {
    primary: 'bg-accent text-nexa-950 hover:bg-accent/90',
    secondary: 'bg-nexa-700 text-nexa-100 hover:bg-nexa-600',
    ghost: 'bg-transparent text-nexa-300 hover:bg-nexa-800 hover:text-nexa-100',
    danger: 'bg-down text-white hover:bg-down/90',
    success: 'bg-up text-white hover:bg-up/90',
  };
  const sizes = {
    sm: 'px-3 py-1.5 text-xs',
    md: 'px-4 py-2 text-sm',
    lg: 'px-6 py-3 text-base',
  };
  return (
    <button
      className={cls(base, variants[variant], sizes[size], className)}
      disabled={disabled || isLoading}
      {...props}
    >
      {isLoading && <span className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />}
      {children}
    </button>
  );
}
