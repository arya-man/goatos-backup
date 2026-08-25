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
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementLoad, ProcurementLoadDetail } from "@/lib/api/procurement";
import { warmupMeta } from "./work-state";

export type SourceEntryDrawerItem = {
  id: string;
  sourceLocation: string;
  sourceParty: string;
  purpose: string;
  expectedCount: number;
  goatsInLoad: string;
  warmup: { label: string; tone: Tone };
  tagging: string;
  hfVaccination: { label: string; tone: Tone };
  healthSelection: { label: string; tone: Tone };
  status: { label: string; tone: Tone };
  detailHref: string;
};

type DrawerDetailResponse = {
  detail: ProcurementLoadDetail;
};

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function daysSince(date: string | null | undefined): number | null {
  if (!date) return null;
  const start = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(start.getTime())) return null;
  const diff = Date.now() - start.getTime();
  return Math.max(0, Math.floor(diff / 86_400_000));
}

function sourceLocationLabel(load: ProcurementLoad, pageContract: AdminUiPageContract): string {
  return load.source_location_name || load.source_location_code || copy(pageContract, "label.holding_not_set");
}

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || load.source_party_id.slice(0, 8);
}

function warmupExpectationKey(purpose: string): string {
  if (purpose === "fattening" || purpose === "non_breeding" || purpose === "breeding") return purpose;
  return "unspecified";
}

function warmupCell(load: ProcurementLoad, detail: ProcurementLoadDetail, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  const purposeValues = Array.from(new Set(detail.goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = detail.goats
    .map((g) => g.warmup_days)
    .filter((d): d is number => typeof d === "number");
  const days = goatDays.length > 0 ? Math.max(...goatDays) : daysSince(load.purchase_date);

  if (purposeValues.length > 1) {
    return {
      label: days === null ? copy(pageContract, "label.mixed_windows") : `${days}d · ${copy(pageContract, "label.mixed")}`,
      tone: "info",
    };
  }

  const purpose = purposeValues[0] ?? "unspecified";
  const warm = warmupMeta(days, purpose);
  const expectationKey = warmupExpectationKey(purpose);
  return {
    label: warm.label === "—" ? copy(pageContract, "label.placeholder") : `${warm.label} / ${optionLabel(pageContract, "warmup_expectations", expectationKey)}`,
    tone: warm.tone,
  };
}

function purposeLabel(detail: ProcurementLoadDetail, pageContract: AdminUiPageContract): string {
  const purposes = Array.from(new Set(detail.goats.map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return copy(pageContract, "label.placeholder");
  if (purposes.length === 1) return optionLabel(pageContract, "proc_purpose", first);
  return copy(pageContract, "label.mixed");
}

function taggingLabel(detail: ProcurementLoadDetail, expectedCount: number): string {
  const tagged = detail.goats.filter((g) => Boolean(g.animal_identifier_1 && g.animal_identifier_2)).length;
  return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(detail: ProcurementLoadDetail, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  const evidence = detail.hf_vaccination_evidence;
  let key = "due";
  if (evidence.some((row) => row.review_status === "trusted")) key = "trusted";
  else if (evidence.some((row) => row.review_status === "imported")) key = "imported";
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) key = "flagged";
  return { label: optionLabel(pageContract, "warmup_evidence_states", key), tone: contractTone(pageContract, "warmup_evidence_states", key) };
}

function healthSelectionLabel(status: ProcurementLoad["status"], pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  let key = "cleared_forward";
  switch (status) {
    case "source_warmup":
      key = "warming";
      break;
    case "health_pending":
      key = "health_pending";
      break;
    case "pre_dispatch_pending":
    case "dispatch_ready":
      key = "selection_ok";
      break;
    case "rejected":
    case "blocked":
      key = "blocked_rejected";
      break;
    case "deferred":
      key = "review";
      break;
  }
  return { label: optionLabel(pageContract, "health_selection_states", key), tone: contractTone(pageContract, "health_selection_states", key) };
}

function drawerItemFromDetail(detail: ProcurementLoadDetail, pageContract: AdminUiPageContract, currentDetailHref: string): SourceEntryDrawerItem {
  const load = detail.load;
  return {
    id: load.load_id,
    sourceLocation: sourceLocationLabel(load, pageContract),
    sourceParty: sourcePartyLabel(load),
    purpose: purposeLabel(detail, pageContract),
    expectedCount: load.expected_count,
    goatsInLoad: String(detail.goats.length),
    warmup: warmupCell(load, detail, pageContract),
    tagging: taggingLabel(detail, load.expected_count),
    hfVaccination: hfVaccinationLabel(detail, pageContract),
    healthSelection: healthSelectionLabel(load.status, pageContract),
    status: {
      label: optionLabel(pageContract, "source_load_status", load.status),
      tone: contractTone(pageContract, "source_load_status", load.status),
    },
    detailHref: currentDetailHref,
  };
}

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
  const [fetchedItems, setFetchedItems] = useState<Record<string, SourceEntryDrawerItem>>({});
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const displayedItem = (displayedId ? fetchedItems[displayedId] : undefined) ?? items.find((item) => item.id === displayedId);
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
    if (!activeId || fetchedItems[activeId]) return undefined;
    const baseItem = items.find((item) => item.id === activeId);
    if (!baseItem) return undefined;
    const controller = new AbortController();
    fetch(`/api/procurement/source-entry/loads/${encodeURIComponent(activeId)}/drawer`, {
      cache: "no-store",
      signal: controller.signal,
    })
      .then((response) => (response.ok ? response.json() : null))
      .then((body: DrawerDetailResponse | null) => {
        if (!body?.detail || controller.signal.aborted) return;
        setFetchedItems((current) => ({
          ...current,
          [activeId]: drawerItemFromDetail(body.detail, pageContract, baseItem.detailHref),
        }));
      })
      .catch(() => {
        // Keep the server-rendered placeholder drawer if the detail fetch is unavailable.
      });
    return () => controller.abort();
  }, [activeId, fetchedItems, items, pageContract]);

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
