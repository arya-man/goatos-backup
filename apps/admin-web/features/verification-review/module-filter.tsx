"use client";

import { type MouseEvent, useEffect, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

type ModuleOption = {
  key: string;
  label: string;
  href: string;
};

export function ModuleFilter({
  allLabel,
  allHref,
  ariaLabel,
  selectedModuleKey,
  modules,
  toxinOption,
}: {
  allLabel: string;
  allHref: string;
  ariaLabel: string;
  selectedModuleKey: string;
  modules: ModuleOption[];
  toxinOption?: { label: string; href: string };
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const navKey = `${pathname}?${searchParams.toString()}`;
  const [optimistic, setOptimistic] = useState<{ key: string; navKey: string } | null>(null);
  const selected = optimistic?.navKey === navKey ? optimistic.key : selectedModuleKey;
  const busy = optimistic?.navKey === navKey;
  const restoreTo = useRef<number | null>(null);
  const busyStartedAt = useRef<number | null>(null);

  useEffect(() => {
    if (busy) return undefined;
    const target = restoreTo.current;
    restoreTo.current = null;
    if (target === null) return undefined;
    window.scrollTo({ top: target, behavior: "instant" as ScrollBehavior });
    return undefined;
  }, [busy]);

  useEffect(() => {
    if (busy) {
      busyStartedAt.current = performance.now();
      document.body.dataset.vrModuleLoading = "1";
      return undefined;
    }

    const startedAt = busyStartedAt.current;
    busyStartedAt.current = null;
    if (startedAt === null) {
      delete document.body.dataset.vrModuleLoading;
      return undefined;
    }

    const remaining = Math.max(0, 420 - (performance.now() - startedAt));
    const timeout = window.setTimeout(() => {
      delete document.body.dataset.vrModuleLoading;
    }, remaining);
    return () => window.clearTimeout(timeout);
  }, [busy]);

  useEffect(() => () => {
    delete document.body.dataset.vrModuleLoading;
  }, []);

  useEffect(() => {
    if (!busy) return undefined;
    const timeout = window.setTimeout(() => {
      setOptimistic(null);
      restoreTo.current = null;
    }, 8000);
    return () => window.clearTimeout(timeout);
  }, [busy]);

  function navigate(event: MouseEvent<HTMLAnchorElement>, key: string, href: string) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    if (key === selected) return;
    restoreTo.current = window.scrollY;
    busyStartedAt.current = performance.now();
    document.body.dataset.vrModuleLoading = "1";
    setOptimistic({ key, navKey });
    router.replace(href, { scroll: false });
  }

  return (
    <div
      className={`vr-legend vr-module-legend${busy ? " busy" : ""}`}
      role="group"
      aria-label={ariaLabel}
      aria-busy={busy}
    >
      <a
        href={allHref}
        className={`vr-lg${selected ? "" : " on"}`}
        aria-current={selected ? undefined : "true"}
        onClick={(event) => navigate(event, "", allHref)}
      >
        {allLabel}
      </a>
      {modules.map((option) => (
        <a
          key={option.key}
          href={option.href}
          className={`vr-lg${selected === option.key ? " on" : ""}`}
          aria-current={selected === option.key ? "true" : undefined}
          onClick={(event) => navigate(event, option.key, option.href)}
        >
          {option.label}
        </a>
      ))}
      {toxinOption ? (
        <a
          href={toxinOption.href}
          className="vr-lg"
          onClick={(event) => navigate(event, "toxin", toxinOption.href)}
        >
          {toxinOption.label}
        </a>
      ) : null}
    </div>
  );
}
