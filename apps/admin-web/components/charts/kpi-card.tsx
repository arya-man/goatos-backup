'use client';

import { memo } from 'react';
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
}

export const KPICard = memo(function KPICard({ label, value, subtitle, icon, delay = 0, variant = 'default' }: KPICardProps) {
  const color = variantColor[variant];

  return (
    <div className={cn(
      "rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors hover:border-[#334155] animate-fade-up",
      delay === 0 && "stagger-1",
      delay === 1 && "stagger-2",
      delay === 2 && "stagger-3",
      delay === 3 && "stagger-4",
      delay === 4 && "stagger-5",
    )}>
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
    </div>
  );
});
