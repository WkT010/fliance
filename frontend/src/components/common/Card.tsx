import { cls } from '@/utils/format';

export function Card({ children, className, title }: { children: React.ReactNode; className?: string; title?: React.ReactNode }) {
  return (
    <div className={cls('rounded border border-nexa-700 bg-nexa-800/50', className)}>
      {title && <div className="border-b border-nexa-700 px-4 py-2 text-sm font-medium text-nexa-100">{title}</div>}
      {children}
    </div>
  );
}
