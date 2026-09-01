"use client";

import { useEffect, useState } from "react";

const TAB_TITLES: Record<string, string> = {
  general: "General",
  breed: "Breed-wise growth",
  birth: "Farm born vs purchased",
  shed: "Elevated vs ground sheds",
  weight: "Road to sale weight",
  time: "Weekly growth",
};

export function WeightsAnalyticsTabLoading({ currentTab }: { currentTab: string }) {
  const [pendingTab, setPendingTab] = useState<string | null>(null);

  useEffect(() => {
    setPendingTab(null);
  }, [currentTab]);

  useEffect(() => {
    if (pendingTab) document.body.classList.add("wt-tab-switching");
    else document.body.classList.remove("wt-tab-switching");
    return () => document.body.classList.remove("wt-tab-switching");
  }, [pendingTab]);

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

  if (!pendingTab) return null;

  return (
    <section className="card wt-tab-skeleton" aria-live="polite" aria-busy="true">
      <div className="wt-skel-head">
        <span className="skel wt-skel-icon" />
        <span className="skel wt-skel-title" aria-label={`Loading ${TAB_TITLES[pendingTab] ?? "tab"}`} />
      </div>
      <span className="skel wt-skel-copy" />
      <span className="skel wt-skel-copy short" />
      <div className="wt-skel-bars">
        {Array.from({ length: pendingTab === "general" ? 5 : 7 }, (_, index) => (
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
