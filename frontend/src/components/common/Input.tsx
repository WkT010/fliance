import { forwardRef } from 'react';
import { cls } from '@/utils/format';

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  /** Icon rendered inside the input, on the left. */
  icon?: React.ReactNode;
  /** Optional suffix rendered inside the input, on the right. */
  suffix?: React.ReactNode;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ label, error, hint, className, icon, suffix, ...props }, ref) => (
    <div className="w-full">
      {label && <label className="mb-1 block text-xs font-medium text-nexa-300">{label}</label>}
      <div className="relative">
        {icon && (
          <span className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-nexa-500">
            {icon}
          </span>
        )}
        <input
          ref={ref}
          className={cls(
            'w-full rounded-lg border bg-nexa-900 px-3 py-2 text-sm text-nexa-100 placeholder-nexa-500 outline-none transition-colors',
            'border-nexa-700 focus:border-accent focus:ring-1 focus:ring-accent/50',
            error && 'border-down focus:border-down focus:ring-down/50',
            icon ? 'pl-9' : null,
            suffix ? 'pr-12' : null,
            className
          )}
          {...props}
        />
        {suffix && (
          <span className="absolute inset-y-0 right-0 flex items-center pr-3 text-xs text-nexa-500">
            {suffix}
          </span>
        )}
      </div>
      {error ? (
        <p className="mt-1 text-xs text-down">{error}</p>
      ) : hint ? (
        <p className="mt-1 text-xs text-nexa-500">{hint}</p>
      ) : null}
    </div>
  )
);
Input.displayName = 'Input';
