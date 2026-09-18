"use client";

import { useEffect, useMemo, useState } from "react";

export function WeightsAnalyticsTabLoading({
  currentTab,
  tabLabels,
}: {
  currentTab: string;
  tabLabels: Record<string, string>;
}) {
  const [pendingTab, setPendingTab] = useState<string | null>(null);
  const tabKeyList = Object.keys(tabLabels).join("|");
  const tabKeys = useMemo(() => tabKeyList.split("|"), [tabKeyList]);
  const activePendingTab = pendingTab === currentTab ? null : pendingTab;

  useEffect(() => {
    if (activePendingTab) document.body.classList.add("wt-tab-switching");
    else document.body.classList.remove("wt-tab-switching");
    return () => document.body.classList.remove("wt-tab-switching");
  }, [activePendingTab]);

  useEffect(() => {
    const onNavigate = (event: Event) => {
      const next = event as CustomEvent<{ value?: string }>;
      // Only the TAB strip drives this skeleton. Other SegmentedLinks on the page (the Feed by
      // weight band card's view and Animals segments) fire the same event with values that are
      // not tabs; treating those as a pending tab switch hid the live tab forever.
      const value = next.detail?.value;
      if (value && value !== currentTab && tabKeys.includes(value)) {
        setPendingTab(value);
      }
    };
    window.addEventListener("metricseg:navigate", onNavigate);
    return () => window.removeEventListener("metricseg:navigate", onNavigate);
  }, [currentTab, tabKeys]);

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
