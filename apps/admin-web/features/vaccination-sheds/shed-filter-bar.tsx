"use client";

import { useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Box from "@mui/material/Box";
import { SearchTextField } from "@/components/app/list/search-text-field";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Server-side search for the shed-wise vaccination board. Reads the LIVE URL via useSearchParams and
// rewrites the `sheds_q` param (resetting `sheds_page`) while preserving every other param (top-bar
// scope, status/capacity chips). Same idiom as the Herd Register filter bar — the actual filtering
// happens server-side on the next render, not client visible-row search. Page-size lives in the pager.
export function ShedFilterBar({
  total,
  pageContract,
}: {
  total: number;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();

  const current = routerSearchParams?.toString() ?? "";
  const searchValue = routerSearchParams?.get("sheds_q") ?? "";

  function onSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const next = new URLSearchParams(current);
    next.delete("sheds_page");
    const value = String(data.get("sheds_q") ?? "").trim();
    if (value) next.set("sheds_q", value);
    else next.delete("sheds_q");
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `/vaccination?${qs}` : "/vaccination", { scroll: false });
    });
  }

  return (
    // Template list toolbar row: keyword search + the result count.
    <Box sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
      <Box component="form" onSubmit={onSearch} sx={{ flex: "1 1 240px", minWidth: 0 }}>
        <SearchTextField name="sheds_q" defaultValue={searchValue} disabled={isPending} placeholder={copy(pageContract, "filter.sheds.search")} />
      </Box>
      <Box component="span" sx={{ ml: "auto", typography: "body2", color: "text.secondary" }}>
        <strong>{total}</strong> {copy(pageContract, "label.sheds_noun")}
      </Box>
    </Box>
  );
}
