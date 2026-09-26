"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { ClipboardCopy, ExternalLink, MapPin } from "lucide-react";

import { RowMenu } from "@/components/app/row-menu";
import Checkbox from "@mui/material/Checkbox";

/**
 * Client islands for the (server-rendered) shed summary table.
 *
 * The table itself stays a server component — it is server-filtered and server-paged — so the two
 * pieces that need client state are lifted out: the selection set (leading checkbox column, with a
 * tri-state header box) and the trailing `⋮` row menu. Selection is deliberately owned by the
 * provider rather than by each checkbox, so a header toggle and the per-row boxes agree, and it is
 * scoped to the rendered page: paging is a server navigation and remounts the provider, which is
 * the honest behaviour when the next page's rows were never in the DOM.
 *
 * READ-ONLY: no action in here mutates anything; the menu navigates or copies.
 */

type SelectionValue = {
  selected: ReadonlySet<string>;
  toggle: (id: string, next: boolean) => void;
  toggleAll: (next: boolean) => void;
  allIds: readonly string[];
};

const SelectionContext = createContext<SelectionValue | null>(null);

export function ShedSelectionProvider({ allIds, children }: { allIds: readonly string[]; children: ReactNode }) {
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const toggle = useCallback((id: string, next: boolean) => {
    setSelected((previous) => {
      const draft = new Set(previous);
      if (next) draft.add(id);
      else draft.delete(id);
      return draft;
    });
  }, []);
  const toggleAll = useCallback(
    (next: boolean) => setSelected(next ? new Set(allIds) : new Set<string>()),
    [allIds],
  );
  const value = useMemo<SelectionValue>(() => ({ selected, toggle, toggleAll, allIds }), [selected, toggle, toggleAll, allIds]);
  return <SelectionContext.Provider value={value}>{children}</SelectionContext.Provider>;
}

function useSelection(): SelectionValue | null {
  return useContext(SelectionContext);
}

export function ShedSelectAllHeader({ label }: { label: string }) {
  const selection = useSelection();
  if (!selection) return null;
  const total = selection.allIds.length;
  const count = selection.selected.size;
  return (
    <Checkbox
      checked={total > 0 && count === total}
      indeterminate={count > 0 && count < total}
      onChange={(event) => selection.toggleAll(event.target.checked)}
      sx={{ p: { xs: 1.5, sm: 1 } }}
      slotProps={{ input: { "aria-label": label } }}
    />
  );
}

export function ShedSelectCheckbox({ id, label }: { id: string; label: string }) {
  const selection = useSelection();
  if (!selection) return null;
  return (
    <Checkbox
      checked={selection.selected.has(id)}
      onChange={(event) => selection.toggle(id, event.target.checked)}
      sx={{ p: { xs: 1.5, sm: 1 } }}
      slotProps={{ input: { "aria-label": label } }}
    />
  );
}

export function ShedRowActions({
  detailHref,
  parkHref,
  shedId,
  labels,
}: {
  detailHref: string;
  parkHref: string;
  shedId: string;
  labels: { menu: string; open: string; park: string; copy: string };
}) {
  const router = useRouter();
  return (
    <RowMenu
      ariaLabel={labels.menu}
      actions={[
        { label: labels.open, icon: <ExternalLink aria-hidden="true" />, onSelect: () => router.push(detailHref, { scroll: false }) },
        { label: labels.park, icon: <MapPin aria-hidden="true" />, onSelect: () => router.push(parkHref, { scroll: false }) },
        {
          label: labels.copy,
          icon: <ClipboardCopy aria-hidden="true" />,
          onSelect: () => {
            void navigator.clipboard?.writeText(shedId);
          },
        },
      ]}
    />
  );
}
