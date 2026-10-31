import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

type PageHeaderProps = {
  eyebrow: string;
  title: string;
  desc: string;
  action?: ReactNode;
};

export function PageHeader({ eyebrow, title, desc, action }: PageHeaderProps) {
  return (
    <header className="page-head">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
        <p>{desc}</p>
      </div>
      {action}
    </header>
  );
}

type MetricCardProps = {
  label: string;
  value: string;
  unit?: string;
  change: string;
  icon: LucideIcon;
};

export function MetricCard({
  label,
  value,
  unit,
  change,
  icon: Icon,
}: MetricCardProps) {
  return (
    <article className="metric card">
      <div className="metric-top">
        <span>{label}</span>
        <span className="metric-icon"><Icon size={17} aria-hidden="true" /></span>
      </div>
      <div className="metric-value">
        {value}
        {unit && <small>{unit}</small>}
      </div>
      <div className="metric-foot">
        <span className="metric-change">
          {change}
        </span>
      </div>
    </article>
  );
}
