import { useState } from 'react';
import { ArrowUpRight, Cpu, SlidersHorizontal, Tag } from 'lucide-react';
import { Models } from './ai/Models';
import { Analysis } from './ai/Analysis';

/**
 * The AI settings surface.
 *
 * Three kinds of thing used to share one page: a runtime you start and stop,
 * a gateway's credentials, and analysis parameters. They are different
 * decisions on different schedules, so they are different tabs — and the
 * fourth, annotators, is not here at all, because writing and running one is
 * work rather than configuration.
 */
export const AISettingsPanel = () => {
  const [tab, setTab] = useState<'models' | 'analysis'>('models');

  return (
    <div className="animate-in fade-in space-y-6 duration-300">
      <nav className="flex gap-1 border-b border-border" role="tablist">
        <Tab active={tab === 'models'} onClick={() => setTab('models')} icon={<Cpu className="h-3.5 w-3.5" />}>
          Models
        </Tab>
        <Tab active={tab === 'analysis'} onClick={() => setTab('analysis')} icon={<SlidersHorizontal className="h-3.5 w-3.5" />}>
          Analysis
        </Tab>

        {/* Annotators live in their own tab, because they are a working
            surface rather than a setting. The pointer is here because this is
            where people will look for them first. */}
        <button
          type="button"
          onClick={() => window.dispatchEvent(new CustomEvent('iql:open-annotators'))}
          className="ml-auto flex items-center gap-1 px-3 py-2 text-xs text-muted-foreground hover:text-foreground"
        >
          <Tag className="h-3.5 w-3.5" />
          Annotators
          <ArrowUpRight className="h-3 w-3" />
        </button>
      </nav>

      {tab === 'models' ? <Models /> : <Analysis />}
    </div>
  );
};

const Tab = ({
  active,
  onClick,
  icon,
  children,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  children: React.ReactNode;
}) => (
  <button
    type="button"
    role="tab"
    aria-selected={active}
    onClick={onClick}
    className={`flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm transition-colors ${
      active
        ? 'border-primary font-semibold text-foreground'
        : 'border-transparent text-muted-foreground hover:text-foreground'
    }`}
  >
    {icon}
    {children}
  </button>
);
