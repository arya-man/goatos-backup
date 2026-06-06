"use client";

import { AlertCircle } from "lucide-react";

interface ErrorStateProps {
  message?: string;
  onRetry?: () => void;
}

export function ErrorState({ message = "Failed to load data", onRetry }: ErrorStateProps) {
  return (
    <div className="flex items-center justify-center py-20">
      <div className="flex flex-col items-center gap-3 text-center">
        <AlertCircle className="h-8 w-8 text-[#f87171]" />
        <p className="text-sm text-[#8899AA]">{message}</p>
        {onRetry && (
          <button
            onClick={onRetry}
            className="rounded-lg bg-[#22262E] px-4 py-2 text-xs font-medium text-[#14F1D9] hover:bg-[#334155] transition-colors"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  );
}
