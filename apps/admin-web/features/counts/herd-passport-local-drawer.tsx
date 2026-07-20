"use client";

import Link from "@/components/no-prefetch-link";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Tag } from "@/components/ui-primitives";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash } from "@/lib/format";
import { HerdReproductiveEdit } from "./herd-actions-ui";

type StatusKind = "lifecycle" | "health" | "breeding";
type StatusTone = "ok" | "warn" | "dng" | "info" | "mut";

export type HerdPassportDrawerItem = {
  goatId: string;
  displayId: string;
  tag1?: string | null;
  tag2?: string | null;
  park: string;
  shed: string;
  breed?: string | null;
  sex?: string | null;
  weightKg?: number | null;
  lifecycleStatus?: string | null;
  healthStatus?: string | null;
  reproductiveStatus?: string | null;
};

function statusTone(value: string | null | undefined, kind: StatusKind): StatusTone {
  const normalized = String(value ?? "").toLowerCase();
  if (!normalized) return "mut";
  if (kind === "health") {
    if (["healthy", "normal", "ok"].includes(normalized)) return "ok";
    if (["sick", "critical", "dead"].includes(normalized)) return "dng";
    if (normalized.includes("treatment") || normalized.includes("watch") || normalized.includes("quarantine")) return "warn";
    return "info";
  }
  if (kind === "breeding") {
    if (normalized.includes("pregnant") || normalized.includes("lactating") || normalized.includes("ai")) return "info";
    if (normalized.includes("open") || normalized.includes("none")) return "mut";
    return "ok";
  }
  if (["alive", "active"].includes(normalized)) return "ok";
  if (["sold", "died", "culled", "lost", "inactive"].includes(normalized)) return "dng";
  return "mut";
}

function weightLabel(weight: number | null | undefined): string {
  return typeof weight === "number" && Number.isFinite(weight) ? `${weight.toFixed(weight % 1 === 0 ? 0 : 1)} kg` : "—";
}

export function HerdPassportLocalDrawer({
  items,
  initialSelectedId,
  closeHref,
  reproductiveIdempotencyKey,
  returnTo,
  pageContract,
}: {
  items: HerdPassportDrawerItem[];
  initialSelectedId?: string;
  closeHref: string;
  reproductiveIdempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const initialItem = items.find((item) => item.goatId === initialSelectedId);
  const [activeId, setActiveId] = useState(initialItem?.goatId);
  const [displayedId, setDisplayedId] = useState(initialItem?.goatId);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const item = items.find((candidate) => candidate.goatId === displayedId);
  const drawerOpen = Boolean(activeId && item);

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("goat_passport") ?? undefined;
    const selected = items.find((candidate) => candidate.goatId === id);
    if (selected) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      setDisplayedId(selected.goatId);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(selected.goatId);
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

  if (!item) return null;
  const cols = tableLabels(pageContract, "herd-register");

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.passport.close_label")}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label={copy(pageContract, "drawer.passport.aria")} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)", fontWeight: 800 }}>G</span>
          <div>
            <div className="mt">{item.displayId}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          <div className="helpgrid" style={{ marginBottom: 12 }}>
            <div className="hk">{cols[0]}</div>
            <div><span className="gid">{item.displayId}</span></div>
            <div className="hk">{cols[1]}</div>
            <div className="mono">{dash(item.tag1)}</div>
            <div className="hk">{cols[2]}</div>
            <div className="mono">{dash(item.tag2)}</div>
          </div>
          <div className="helpgrid">
            <div className="hk">{cols[3]}</div><div>{item.park}</div>
            <div className="hk">{cols[4]}</div><div>{item.shed}</div>
            <div className="hk">{cols[5]}</div><div>{dash(item.breed)}</div>
            <div className="hk">{cols[6]}</div><div>{dash(item.sex)}</div>
            <div className="hk">{cols[7]}</div><div>{weightLabel(item.weightKg)}</div>
            <div className="hk">{cols[8]}</div><div><Tag tone={statusTone(item.lifecycleStatus, "lifecycle")}>{dash(item.lifecycleStatus)}</Tag></div>
            <div className="hk">{cols[9]}</div><div><Tag tone={statusTone(item.healthStatus, "health")}>{dash(item.healthStatus)}</Tag></div>
            <div className="hk">{cols[10]}</div>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <Tag tone={statusTone(item.reproductiveStatus, "breeding")}>{dash(item.reproductiveStatus)}</Tag>
              <HerdReproductiveEdit
                goatId={item.goatId}
                displayId={item.displayId}
                currentStatus={item.reproductiveStatus}
                idempotencyKey={reproductiveIdempotencyKey}
                returnTo={returnTo}
                pageContract={pageContract}
              />
            </div>
          </div>
        </div>
        <div className="df">
          <Link href={`/goats/${encodeURIComponent(item.goatId)}`} className="btn p">{copy(pageContract, "action.full_change_history")}</Link>
          <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
        </div>
      </aside>
    </>
  );
}
