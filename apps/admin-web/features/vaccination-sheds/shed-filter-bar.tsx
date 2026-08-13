"use client";

import { useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Search } from "lucide-react";
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
    <div className="tbar" style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }}>
      <form onSubmit={onSearch} className="tsearch" style={{ margin: 0, minWidth: 260, flex: "1 1 280px" }}>
        <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
        <input
          name="sheds_q"
          defaultValue={searchValue}
          disabled={isPending}
          placeholder={copy(pageContract, "filter.sheds.search")}
          aria-label={copy(pageContract, "filter.sheds.search")}
        />
      </form>
      <span className="muted small">
        {total} {copy(pageContract, "label.sheds_noun")}
      </span>
    </div>
  );
}
