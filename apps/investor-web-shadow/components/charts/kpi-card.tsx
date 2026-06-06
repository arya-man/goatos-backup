'use client';

import { cn } from '@/lib/utils';

interface KPICardProps {
  label: string;
  value: string;
  subtitle?: string;
  icon: React.ReactNode;
  delay?: number;
}

export function KPICard({ label, value, subtitle, icon, delay = 0 }: KPICardProps) {
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
          <p className="text-[10px] font-medium uppercase tracking-wider text-[#14F1D9]">{label}</p>
          <p className="mt-1.5 text-2xl font-bold text-[#FFFFFF]">{value}</p>
          {subtitle && <p className="mt-1 text-xs text-[#8899AA]">{subtitle}</p>}
        </div>
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E] text-[#14F1D9]">
          {icon}
        </div>
      </div>
    </div>
  );
}
