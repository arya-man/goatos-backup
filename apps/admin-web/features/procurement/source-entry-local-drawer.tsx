"use client";

import Link from "@/components/no-prefetch-link";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { PackageSearch, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export type SourceEntryDrawerItem = {
  id: string;
  sourceLocation: string;
  sourceParty: string;
  purpose: string;
  expectedCount: number;
  goatsInLoad: number;
  warmup: { label: string; tone: Tone };
  tagging: string;
  hfVaccination: { label: string; tone: Tone };
  healthSelection: { label: string; tone: Tone };
  status: { label: string; tone: Tone };
  detailHref: string;
};

export function SourceEntryLocalDrawer({
  items,
  initialSelectedId,
  closeHref,
  loadLabels,
  pageContract,
}: {
  items: SourceEntryDrawerItem[];
  initialSelectedId?: string;
  closeHref: string;
  loadLabels: string[];
  pageContract: AdminUiPageContract;
}) {
  const initialItem = items.find((item) => item.id === initialSelectedId);
  const [activeId, setActiveId] = useState(initialItem?.id);
  const [displayedId, setDisplayedId] = useState(initialItem?.id);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const displayedItem = items.find((item) => item.id === displayedId);
  const drawerOpen = Boolean(activeId && displayedItem);

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("source_load") ?? undefined;
    const item = items.find((candidate) => candidate.id === id);
    if (item) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      setDisplayedId(item.id);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(item.id);
        openFrameRef.current = null;
      });
      return;
    }
    setActiveId(undefined);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedId(undefined);
      closeTimerRef.current = null;
      triggerRef.current?.focus();
    }, 280);
  }, [items]);

  useEffect(() => {
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
    window.addEventListener("popstate", syncFromUrl);
    return () => {
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
      window.removeEventListener("popstate", syncFromUrl);
      if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, [syncFromUrl]);

  useEffect(() => {
    if (!drawerOpen) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    setActiveId(undefined);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  if (!displayedItem) return null;

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.load.close_label")}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label={copy(pageContract, "drawer.load.aria")} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--info)" }}>
            <PackageSearch className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.load.eyebrow")}</div>
            <h2>{copy(pageContract, "drawer.load.title_prefix")} — {displayedItem.sourceLocation}</h2>
            <div className="muted small" style={{ marginTop: 3 }}>
              {displayedItem.sourceParty} · {displayedItem.purpose}
            </div>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.load.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          <div className="helpgrid">
            <div className="hk">{loadLabels[0]}</div>
            <div>{displayedItem.id.slice(0, 8)}</div>
            <div className="hk">{copy(pageContract, "drawer.load.supplier")}</div>
            <div>{displayedItem.sourceParty}</div>
            <div className="hk">{copy(pageContract, "drawer.load.holding_farm")}</div>
            <div>{displayedItem.sourceLocation}</div>
            <div className="hk">{copy(pageContract, "drawer.load.expected_animals")}</div>
            <div>{displayedItem.expectedCount}</div>
            <div className="hk">{copy(pageContract, "drawer.load.goats_in_load")}</div>
            <div>{displayedItem.goatsInLoad}</div>
            <div className="hk">{loadLabels[4]}</div>
            <div><Tag tone={displayedItem.warmup.tone}>{displayedItem.warmup.label}</Tag></div>
            <div className="hk">{loadLabels[5]}</div>
            <div><Tag tone="mut">{displayedItem.tagging}</Tag></div>
            <div className="hk">{loadLabels[6]}</div>
            <div><Tag tone={displayedItem.hfVaccination.tone}>{displayedItem.hfVaccination.label}</Tag></div>
            <div className="hk">{loadLabels[7]}</div>
            <div><Tag tone={displayedItem.healthSelection.tone}>{displayedItem.healthSelection.label}</Tag></div>
            <div className="hk">{loadLabels[8]}</div>
            <div><Tag tone={displayedItem.status.tone}>{displayedItem.status.label}</Tag></div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.load.note")}</div>
        </div>
        <div className="df">
          <Link href={displayedItem.detailHref} className="btn p">{copy(pageContract, "action.open_load_actions")}</Link>
          <Link href={`${displayedItem.detailHref}#hf-evidence`} className="btn">{copy(pageContract, "action.record_hf_evidence")}</Link>
          <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
        </div>
      </aside>
    </>
  );
}
