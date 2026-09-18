"use client";

import { Layers, Lock, Pencil, Plus, Search } from "lucide-react";
import { useState } from "react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";

/**
 * The Lists panel of Items & categories (the prototype's middle column): "All items", the
 * built-in catalogues (locked: they can be renamed, never removed) and the farm's own lists,
 * each with the number of items it holds. Choosing a list is a real navigation (the items table
 * is filtered server-side, whole subtree); the search box and the Active / Archived toggle
 * only narrow the list of NAMES already on screen, which is why they are local state.
 *
 * Renders no copy of its own: every label arrives resolved from the page contract, and every
 * href is precomputed by the server component.
 */

export type CatalogueList = {
  id: string;
  name: string;
  depth: number;
  /** Under CATALOGUES (its root is built in) rather than YOUR LISTS. */
  builtin: boolean;
  /** The row itself is built in: shown with the lock. */
  locked: boolean;
  archived: boolean;
  count: number;
  href: string;
  editHref: string;
};

export function CatalogueLists({
  copy,
  lists,
  allHref,
  allCount,
  current,
  canEdit,
  newHref,
}: {
  copy: Record<string, string>;
  lists: CatalogueList[];
  allHref: string;
  allCount: number;
  /** The selected list id, "" for All items. */
  current: string;
  canEdit: boolean;
  newHref: string;
}) {
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<"active" | "archived">("active");
  const needle = q.trim().toLowerCase();
  const shown = lists.filter((list) => (status === "archived" ? list.archived : !list.archived) && (!needle || list.name.toLowerCase().includes(needle)));
  const catalogues = shown.filter((list) => list.builtin);
  const yours = shown.filter((list) => !list.builtin);
  const row = (list: CatalogueList) => (
    <div key={list.id} className={list.id === current ? "cfg-list on" : "cfg-list"} style={{ paddingLeft: 10 + list.depth * 14 }}>
      <Link href={list.href} scroll={false} className="cfg-list-link" aria-current={list.id === current ? "page" : undefined}>
        {list.locked ? <Lock className="ic" aria-hidden="true" /> : null}
        <span className="cfg-list-name">{list.name}</span>
        <span className="cfg-list-count">{list.count}</span>
      </Link>
      {canEdit ? (
        <LocalOverlayLink href={list.editHref} scroll={false} className="cfg-list-edit" aria-label={`${copy["action.edit_row.label"]} ${list.name}`}>
          <Pencil className="ic" aria-hidden="true" />
        </LocalOverlayLink>
      ) : null}
    </div>
  );
  return (
    <aside className="card cfg-lists" aria-label={copy["lists.title"]}>
      <div className="hd">
        <h3>
          {copy["lists.title"]} <span className="cfg-count">{lists.filter((l) => !l.archived).length}</span>
        </h3>
      </div>
      <div className="cfg-lists-bar">
        <label className="tsearch">
          <Search className="ic" aria-hidden="true" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={copy["lists.search"]} aria-label={copy["lists.search"]} />
        </label>
        <div className="subtabs" aria-label={copy["column.status"]}>
          <button type="button" className={status === "active" ? "on" : ""} onClick={() => setStatus("active")}>
            {copy["status.active"]}
          </button>
          <button type="button" className={status === "archived" ? "on" : ""} onClick={() => setStatus("archived")}>
            {copy["status.archived"]}
          </button>
        </div>
      </div>
      <div className="cfg-lists-body">
        {status === "active" && !needle ? (
          <div className={current === "" ? "cfg-list on" : "cfg-list"}>
            <Link href={allHref} scroll={false} className="cfg-list-link" aria-current={current === "" ? "page" : undefined}>
              <Layers className="ic" aria-hidden="true" />
              <span className="cfg-list-name">{copy["lists.all"]}</span>
              <span className="cfg-list-count">{allCount}</span>
            </Link>
          </div>
        ) : null}
        {catalogues.length ? (
          <>
            <div className="cfg-lists-title">
              <Lock className="ic" aria-hidden="true" /> {copy["lists.catalogues"]}
            </div>
            {catalogues.map(row)}
          </>
        ) : null}
        <div className="cfg-lists-title">{copy["lists.yours"]}</div>
        {yours.map(row)}
        {canEdit ? (
          <LocalOverlayLink href={newHref} scroll={false} className="cfg-list-new">
            <Plus className="ic" aria-hidden="true" /> {copy["lists.new"]}
          </LocalOverlayLink>
        ) : null}
      </div>
    </aside>
  );
}
