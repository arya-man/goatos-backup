"use client";

import { useEffect, useRef, useState } from "react";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
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

  function navigate(key: string, href: string) {
    if (key === selected) return;
    restoreTo.current = window.scrollY;
    busyStartedAt.current = performance.now();
    document.body.dataset.vrModuleLoading = "1";
    setOptimistic({ key, navKey });
    router.replace(href, { scroll: false });
  }

  // The module picker sits in the queue toolbar as the template list toolbar's leading select
  // (InvoiceTableToolbar "Service" / OrderTableToolbar anatomy). It navigates on change with the
  // scroll-preserving `router.replace` above rather than riding the shed form's Apply.
  const hrefByKey = new Map<string, string>([["", allHref], ...modules.map((option) => [option.key, option.href] as [string, string])]);
  if (toxinOption) hrefByKey.set("toxin", toxinOption.href);
  const options = [
    { value: "", label: allLabel },
    ...modules.map((option) => ({ value: option.key, label: option.label })),
    ...(toxinOption ? [{ value: "toxin", label: toxinOption.label }] : []),
  ];
  return (
    <div className={`vr-module-legend${busy ? " busy" : ""}`} role="group" aria-label={ariaLabel} aria-busy={busy}>
      <TextField
        select
        label={ariaLabel}
        value={options.some((option) => option.value === selected) ? selected : ""}
        onChange={(event) => {
          const key = event.target.value;
          const href = hrefByKey.get(key);
          if (href) navigate(key, href);
        }}
        sx={{ width: { xs: 1, md: 180 }, flexShrink: 0 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.value || "all"} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
    </div>
  );
}
