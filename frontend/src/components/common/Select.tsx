import { forwardRef } from 'react';
import { cls } from '@/utils/format';

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  options?: { value: string; label: string }[];
  error?: string;
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ label, options, className, error, children, ...props }, ref) => (
    <div className="w-full">
      {label && <label className="mb-1 block text-xs font-medium text-nexa-300">{label}</label>}
      <div className="relative">
        <select
          ref={ref}
          className={cls(
            'w-full appearance-none rounded-lg border bg-nexa-900 px-3 py-2 pr-9 text-sm text-nexa-100 outline-none transition-colors',
            'border-nexa-700 focus:border-accent focus:ring-1 focus:ring-accent/50',
            error && 'border-down focus:border-down focus:ring-down/50',
            className
          )}
          {...props}
        >
          {options ? options.map((o) => (
            <option key={o.value} value={o.value}>{o.label}</option>
          )) : children}
        </select>
        <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2 text-nexa-500">
          <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4">
            <path d="M6 9l6 6 6-6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      </div>
      {error && <p className="mt-1 text-xs text-down">{error}</p>}
    </div>
  )
);
Select.displayName = 'Select';
