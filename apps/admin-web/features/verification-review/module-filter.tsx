"use client";

import { MODULE_FILTER_WIDTH } from "./verification-layout";
import { useEffect, useRef, useState } from "react";
import Box from "@mui/material/Box";
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

  useEffect(() => {
    if (busy) return undefined;
    const target = restoreTo.current;
    restoreTo.current = null;
    if (target === null) return undefined;
    window.scrollTo({ top: target, behavior: "instant" as ScrollBehavior });
    return undefined;
  }, [busy]);

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
    // While the pick is in flight the queue below is already its UrlSuspense TableSkeleton; the
    // select only shows the optimistic value and a progress cursor.
    <Box role="group" aria-label={ariaLabel} aria-busy={busy} sx={{ cursor: busy ? "progress" : undefined }}>
      <TextField
        select
        label={ariaLabel}
        value={options.some((option) => option.value === selected) ? selected : ""}
        onChange={(event) => {
          const key = event.target.value;
          const href = hrefByKey.get(key);
          if (href) navigate(key, href);
        }}
        sx={{ width: { xs: 1, md: MODULE_FILTER_WIDTH }, flexShrink: 0 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.value || "all"} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
    </Box>
  );
}
