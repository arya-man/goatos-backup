'use client';

import { cn } from '@/lib/utils';

interface ChartCardProps {
  title: React.ReactNode;
  subtitle?: string;
  children: React.ReactNode;
  className?: string;
}

export function ChartCard({ title, subtitle, children, className }: ChartCardProps) {
  return (
    <div className={cn(
      "rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors hover:border-[#334155]",
      className
    )}>
      <div className="mb-4">
        <h3 className="text-sm font-bold text-[#FFFFFF]">{title}</h3>
        {subtitle && <p className="text-[11px] text-[#14F1D9] mt-0.5">{subtitle}</p>}
      </div>
      {children}
    </div>
  );
}
