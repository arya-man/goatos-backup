"use client";

export function LoadingState() {
  return (
    <div className="flex items-center justify-center py-20">
      <div className="flex flex-col items-center gap-3">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-[#334155] border-t-[#14F1D9]" />
        <p className="text-xs text-[#8899AA]">Loading data...</p>
      </div>
    </div>
  );
}

export function LoadingSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-4">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="animate-pulse">
          <div className="h-32 rounded-xl bg-[#22262E]" />
        </div>
      ))}
    </div>
  );
}
