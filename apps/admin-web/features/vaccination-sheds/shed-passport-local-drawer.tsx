"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { GoatPassportResponse } from "@/lib/api/server";
import type { VaccinationShedAnimalRow } from "@/lib/api/vaccination-sheds";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash } from "@/lib/format";
import { X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type GoatPassport = GoatPassportResponse["goat"];

function animalId(row: VaccinationShedAnimalRow): string {
  return row.goatId;
}

type PassportReadResult =
  | { ok: true; data: GoatPassportResponse }
  | { ok: false; error: string };

async function getShedGoatPassport(goatId: string): Promise<PassportReadResult> {
  const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/passport`, {
    headers: { Accept: "application/json" },
    cache: "no-store",
  });
  const payload = await response.json() as Partial<GoatPassportResponse> & { error?: string };
  if (!response.ok || !payload.goat) {
    return { ok: false, error: payload.error ?? `passport_read_${response.status}` };
  }
  return { ok: true, data: payload as GoatPassportResponse };
}

function statusTone(value: string | null | undefined, kind: "lifecycle" | "health" | "breeding"): "ok" | "warn" | "dng" | "info" | "mut" {
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

export function ShedPassportLocalDrawer({
  rows,
  initialSelectedGoatId,
  closeHref,
  pageContract,
}: {
  rows: VaccinationShedAnimalRow[];
  initialSelectedGoatId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: rows,
    itemId: animalId,
    selectionKey: "goat_passport",
    initialSelectedId: initialSelectedGoatId,
    closeHref,
  });
  const [passports, setPassports] = useState<Record<string, GoatPassport>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const requestsRef = useRef(new Map<string, ReturnType<typeof getShedGoatPassport>>());

  useEffect(() => {
    if (!displayedItem || passports[displayedItem.goatId] || errors[displayedItem.goatId]) return;
    const goatId = displayedItem.goatId;
    let active = true;
    const request = requestsRef.current.get(goatId) ?? getShedGoatPassport(goatId);
    requestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setPassports((current) => ({ ...current, [goatId]: result.data.goat }));
      else setErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, errors, passports]);

  if (!displayedItem) return null;
  const goat = passports[displayedItem.goatId];
  const error = errors[displayedItem.goatId];

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
            <div className="mt">{goat?.display_id ?? displayedItem.displayId}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          {error ? (
            <div className="alert"><b>{copy(pageContract, "fallback.title")}</b>&nbsp;{error}</div>
          ) : (
            <>
              <div className="helpgrid" style={{ marginBottom: 12 }}>
                <div className="hk">{copy(pageContract, "label.display_id")}</div>
                <div><span className="gid">{goat?.display_id ?? displayedItem.displayId}</span></div>
                <div className="hk">{copy(pageContract, "label.tag_1")}</div>
                <div className="mono">{dash(goat?.summary.animal_identifier_1 ?? displayedItem.tag1)}</div>
                <div className="hk">{copy(pageContract, "label.tag_2")}</div>
                <div className="mono">{dash(goat?.summary.animal_identifier_2 ?? displayedItem.tag2)}</div>
              </div>
              <div className="helpgrid">
                <div className="hk">{copy(pageContract, "label.location")}</div>
                <div>{dash(goat?.summary.location_path.display)}</div>
                <div className="hk">{copy(pageContract, "label.breed_sex")}</div>
                <div>{dash(goat ? [goat.summary.breed, goat.summary.sex].filter(Boolean).join(" / ") : [displayedItem.breed, displayedItem.sex].filter(Boolean).join(" / "))}</div>
                <div className="hk">{copy(pageContract, "label.lifecycle")}</div>
                <div><Tag tone={statusTone(goat?.summary.lifecycle_status ?? displayedItem.lifecycleStatus, "lifecycle")}>{dash(goat?.summary.lifecycle_status ?? displayedItem.lifecycleStatus)}</Tag></div>
                <div className="hk">{copy(pageContract, "label.health")}</div>
                <div><Tag tone={statusTone(goat?.summary.health_status ?? displayedItem.healthStatus, "health")}>{dash(goat?.summary.health_status ?? displayedItem.healthStatus)}</Tag></div>
                <div className="hk">{copy(pageContract, "label.reproductive")}</div>
                <div><Tag tone={statusTone(goat?.summary.reproductive_status, "breeding")}>{dash(goat?.summary.reproductive_status)}</Tag></div>
              </div>
              {!goat ? <p className="muted small" style={{ marginTop: 14 }} aria-live="polite">…</p> : null}
            </>
          )}
        </div>
        <div className="df">
          <Link href={`/goats/${encodeURIComponent(displayedItem.goatId)}`} className="btn p">
            {copy(pageContract, "action.full_change_history")}
          </Link>
          <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
        </div>
      </aside>
    </>
  );
}
