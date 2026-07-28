import { forwardRef } from 'react';
import { cls } from '@/utils/format';

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ label, error, className, ...props }, ref) => (
    <div className="w-full">
      {label && <label className="mb-1 block text-xs font-medium text-nexa-300">{label}</label>}
      <input
        ref={ref}
        className={cls(
          'w-full rounded border bg-nexa-900 px-3 py-2 text-sm text-nexa-100 placeholder-nexa-500 outline-none transition-colors',
          'border-nexa-700 focus:border-accent focus:ring-1 focus:ring-accent/50',
          error && 'border-down focus:border-down focus:ring-down/50',
          className
        )}
        {...props}
      />
      {error && <p className="mt-1 text-xs text-down">{error}</p>}
    </div>
  )
);
Input.displayName = 'Input';
