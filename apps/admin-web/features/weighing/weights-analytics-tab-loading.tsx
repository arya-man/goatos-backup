"use client";

import { useEffect, useState } from "react";

export function WeightsAnalyticsTabLoading({
  currentTab,
  tabLabels,
}: {
  currentTab: string;
  tabLabels: Record<string, string>;
}) {
  const [pendingTab, setPendingTab] = useState<string | null>(null);
  const activePendingTab = pendingTab === currentTab ? null : pendingTab;

  useEffect(() => {
    if (activePendingTab) document.body.classList.add("wt-tab-switching");
    else document.body.classList.remove("wt-tab-switching");
    return () => document.body.classList.remove("wt-tab-switching");
  }, [activePendingTab]);

  useEffect(() => {
    const onNavigate = (event: Event) => {
      const next = event as CustomEvent<{ value?: string }>;
      if (next.detail?.value && next.detail.value !== currentTab) {
        setPendingTab(next.detail.value);
      }
    };
    window.addEventListener("metricseg:navigate", onNavigate);
    return () => window.removeEventListener("metricseg:navigate", onNavigate);
  }, [currentTab]);

  if (!activePendingTab) return null;

  return (
    <section className="card wt-tab-skeleton" aria-live="polite" aria-busy="true">
      <div className="wt-skel-head">
        <span className="skel wt-skel-icon" />
        <span className="skel wt-skel-title" aria-label={tabLabels[activePendingTab] ?? ""} />
      </div>
      <span className="skel wt-skel-copy" />
      <span className="skel wt-skel-copy short" />
      <div className="wt-skel-bars">
        {Array.from({ length: activePendingTab === "general" ? 5 : 7 }, (_, index) => (
          <div className="wt-skel-row" key={index}>
            <span className="skel wt-skel-label" />
            <span className="skel wt-skel-bar" />
            <span className="skel wt-skel-value" />
          </div>
        ))}
      </div>
    </section>
  );
}
