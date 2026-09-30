"use client";

import Button from "@mui/material/Button";
import { LinkButton } from "@/components/app/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem } from "@/components/app/detail-drawer";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Iconify } from "@/components/minimal/iconify";
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
  const goats = detail.goats ?? [];
  const purposeValues = Array.from(new Set(goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = goats
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
  const purposes = Array.from(new Set((detail.goats ?? []).map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return copy(pageContract, "label.placeholder");
  if (purposes.length === 1) return optionLabel(pageContract, "proc_purpose", first);
  return copy(pageContract, "label.mixed");
}

function taggingLabel(detail: ProcurementLoadDetail, expectedCount: number): string {
  const tagged = (detail.goats ?? []).filter((g) => Boolean(g.animal_identifier_1 && g.animal_identifier_2)).length;
  return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(detail: ProcurementLoadDetail, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  const evidence = detail.hf_vaccination_evidence ?? [];
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
    goatsInLoad: String((detail.goats ?? []).length),
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

  const closeDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    setActiveId(undefined);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  // Escape and the backdrop reach closeDrawer through the template Drawer's onClose (focus is
  // trapped in the paper and returned to the row); a second Escape listener would step history
  // back twice.

  if (!displayedItem) return null;

  const closeLabel = copy(pageContract, "drawer.load.close_label");
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={`${copy(pageContract, "drawer.load.title_prefix")} — ${displayedItem.sourceLocation}`}
      eyebrow={copy(pageContract, "drawer.load.eyebrow")}
      icon={<Iconify icon="solar:box-minimalistic-bold" aria-hidden="true" />}
      iconColors={{ bg: "var(--brand-soft)", fg: "var(--info)" }}
      subtitle={`${displayedItem.sourceParty} · ${displayedItem.purpose}`}
      ariaLabel={copy(pageContract, "drawer.load.aria")}
      closeLabel={closeLabel}
      footer={
        <>
          <Button type="button" variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
          <LinkButton href={`${displayedItem.detailHref}#hf-evidence`} variant="outlined" color="inherit">{copy(pageContract, "action.record_hf_evidence")}</LinkButton>
          <LinkButton href={displayedItem.detailHref} variant="contained">{copy(pageContract, "action.open_load_actions")}</LinkButton>
        </>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={loadLabels[0]}>{displayedItem.id.slice(0, 8)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.load.supplier")}>{displayedItem.sourceParty}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.load.holding_farm")}>{displayedItem.sourceLocation}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.load.expected_animals")}>{displayedItem.expectedCount}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.load.goats_in_load")}>{displayedItem.goatsInLoad}</DrawerMetaItem>
        <DrawerMetaItem label={loadLabels[4]}><Tag tone={displayedItem.warmup.tone}>{displayedItem.warmup.label}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={loadLabels[5]}><Tag tone="mut">{displayedItem.tagging}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={loadLabels[6]}><Tag tone={displayedItem.hfVaccination.tone}>{displayedItem.hfVaccination.label}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={loadLabels[7]}><Tag tone={displayedItem.healthSelection.tone}>{displayedItem.healthSelection.label}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={loadLabels[8]}><Tag tone={displayedItem.status.tone}>{displayedItem.status.label}</Tag></DrawerMetaItem>
      </DrawerMetaGrid>
    </DetailDrawer>
  );
}
