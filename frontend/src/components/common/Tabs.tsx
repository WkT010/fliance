import { useState } from 'react';
import { cls } from '@/utils/format';

interface Tab {
  id: string;
  label: string;
  content: React.ReactNode;
}

export function Tabs({
  tabs,
  defaultTab,
  activeTab,
  onChange,
}: {
  tabs: Tab[];
  defaultTab?: string;
  activeTab?: string;
  onChange?: (id: string) => void;
}) {
  const [internalActive, setInternalActive] = useState(defaultTab || tabs[0]?.id);
  const active = activeTab ?? internalActive;
  const setActive = (id: string) => {
    setInternalActive(id);
    onChange?.(id);
  };
  return (
    <div className="flex h-full flex-col">
      <div className="flex border-b border-nexa-700">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setActive(t.id)}
            className={cls(
              'px-4 py-2 text-sm font-medium transition-colors',
              active === t.id ? 'border-b-2 border-accent text-accent' : 'text-nexa-400 hover:text-nexa-100'
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="flex-1 overflow-auto p-4">
        {tabs.find((t) => t.id === active)?.content}
      </div>
    </div>
  );
}
