import { cls } from '@/utils/format';

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  options?: { value: string; label: string }[];
}

export function Select({ label, options, className, children, ...props }: SelectProps) {
  return (
    <div className="w-full">
      {label && <label className="mb-1 block text-xs font-medium text-nexa-300">{label}</label>}
      <select
        className={cls(
          'w-full rounded border border-nexa-700 bg-nexa-900 px-3 py-2 text-sm text-nexa-100 outline-none focus:border-accent focus:ring-1 focus:ring-accent/50',
          className
        )}
        {...props}
      >
        {options ? options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        )) : children}
      </select>
    </div>
  );
}
