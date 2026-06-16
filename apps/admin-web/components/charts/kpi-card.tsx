'use client';

import { memo } from 'react';
import Link from 'next/link';
import { cn } from '@/lib/utils';

type KPIVariant = 'default' | 'positive' | 'negative' | 'amber';

const variantColor: Record<KPIVariant, string> = {
  default:  '#14F1D9',
  positive: '#4ade80',
  negative: '#f87171',
  amber:    '#fb923c',
};

interface KPICardProps {
  label: string;
  value: string;
  subtitle?: string;
  icon: React.ReactNode;
  delay?: number;
  variant?: KPIVariant;
  /** When set, the whole card becomes a navigation link. */
  href?: string;
  /** Accessible label for the link target (defaults to "Open {label}"). */
  navLabel?: string;
}

export const KPICard = memo(function KPICard({ label, value, subtitle, icon, delay = 0, variant = 'default', href, navLabel }: KPICardProps) {
  const color = variantColor[variant];
  void delay;

  const body = (
    <div className="flex items-start justify-between">
      <div>
        <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color }}>{label}</p>
        <p className="mt-1.5 text-2xl font-bold text-[#FFFFFF]">{value}</p>
        {subtitle && <p className="mt-1 text-xs text-[#8899AA]">{subtitle}</p>}
      </div>
      <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E]" style={{ color }}>
        {icon}
      </div>
    </div>
  );

  if (href) {
    return (
      <Link
        href={href}
        aria-label={navLabel ?? `Open ${label}`}
        className={cn(
          "block rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors",
          "hover:border-[#14F1D9]/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#14F1D9]/60",
        )}
      >
        {body}
      </Link>
    );
  }

  return (
    <div className={cn(
      "rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors hover:border-[#334155]",
    )}>
      {body}
    </div>
  );
});
