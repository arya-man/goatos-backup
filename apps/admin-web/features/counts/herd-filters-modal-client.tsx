"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { ListFilter, Search } from "lucide-react";
import { useState, useTransition } from "react";
import TextField from "@mui/material/TextField";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import InputAdornment from "@mui/material/InputAdornment";
import { HerdFiltersModal } from "./herd-filters-modal";
import type { RouteSearchParams } from "@/lib/search-params";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Filters button + modal. Reads the LIVE URL via useSearchParams (not a server-render snapshot) so the
// modal's initial chip state and preserved scope always reflect the current address bar, even after a
// client-side filter navigation without a full reload.
export function HerdFiltersModalClient({
  hasFilters,
  pageContract,
}: {
  hasFilters?: boolean;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const [isOpen, setIsOpen] = useState(false);

  const params: RouteSearchParams = {};
  routerSearchParams?.forEach((value, key) => {
    const existing = params[key];
    if (existing === undefined) {
      params[key] = value;
    } else if (Array.isArray(existing)) {
      existing.push(value);
    } else {
      params[key] = [existing, value];
    }
  });
  const searchValue = typeof params.q === "string" ? params.q : Array.isArray(params.q) ? params.q[0] : "";

  function paramsWith(updates: Record<string, string | null>): string {
    const next = new URLSearchParams(routerSearchParams?.toString() ?? "");
    next.delete("cursor");
    next.delete("page");
    for (const [key, value] of Object.entries(updates)) {
      if (value === null || value === "") next.delete(key);
      else next.set(key, value);
    }
    const qs = next.toString();
    return qs ? `/counts/herd?${qs}` : "/counts/herd";
  }

  function onSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    startTransition(() => {
      router.replace(paramsWith({ q: String(data.get("q") ?? "").trim() || null }), { scroll: false });
    });
  }

  // Template UserTableToolbar: an outlined search field with a start adornment and the Filters
  // button (template job/product filters: inherit Button + Badge dot while filters are active).
  // Rows per page lives in the table pager only, never a second select up here.
  return (
    <>
      <Box
        sx={{
          px: 2.5,
          pb: 2.5,
          gap: 2,
          display: "flex",
          flexDirection: { xs: "column", sm: "row" },
          alignItems: { xs: "stretch", sm: "center" },
          opacity: isPending ? 0.6 : 1,
        }}
      >
        <Box component="form" onSubmit={onSearch} sx={{ flex: "1 1 auto", minWidth: 0 }}>
          <TextField
            fullWidth
            name="q"
            defaultValue={searchValue}
            disabled={isPending}
            placeholder={copy(pageContract, "filter.toolbar_placeholder")}
            slotProps={{
              htmlInput: { "aria-label": copy(pageContract, "filter.toolbar_aria") },
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <Search size={20} aria-hidden="true" />
                  </InputAdornment>
                ),
              },
            }}
          />
        </Box>
        <Button
          color="inherit"
          onClick={() => setIsOpen(true)}
          startIcon={
            <Badge color="error" variant="dot" invisible={!hasFilters}>
              <ListFilter size={20} aria-hidden="true" />
            </Badge>
          }
          aria-label={hasFilters ? `${copy(pageContract, "action.filters")} · ${copy(pageContract, "filter.active_badge")}` : undefined}
          sx={{ flexShrink: 0, minHeight: { xs: 44, sm: 36 } }}
        >
          {copy(pageContract, "action.filters")}
        </Button>
      </Box>

      <HerdFiltersModal open={isOpen} pageContract={pageContract} searchParams={params} onClose={() => setIsOpen(false)} />
    </>
  );
}
