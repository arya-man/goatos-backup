"use client";

import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { VaccinationPassport } from "@/lib/api/server";
import type { LiveTrackerComboRow } from "@/lib/api/vaccination-live-tracker";
import { fmtDate } from "@/lib/format";

// Goat Passport drawer for the combo-dose rows. Every admin-web table that lists individual animals
// opens this same local overlay rather than navigating away, so the board keeps its live state while
// the animal is inspected.

function rowId(row: LiveTrackerComboRow): string {
  return row.goat_id;
}

async function readVaccinationPassport(goatId: string): Promise<{ ok: true; data: VaccinationPassport } | { ok: false; error: string }> {
  try {
    const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/vaccination-passport`, {
      headers: { Accept: "application/json" },
      cache: "no-store",
    });
    const payload = (await response.json().catch(() => ({}))) as Partial<VaccinationPassport> & { error?: string };
    if (!response.ok || !payload.goat_id) {
      return { ok: false, error: payload.error ?? `vaccination_passport_read_${response.status}` };
    }
    return { ok: true, data: payload as VaccinationPassport };
  } catch {
    return { ok: false, error: "vaccination_passport_unreachable" };
  }
}

function obligationTone(status: string): "ok" | "warn" | "dng" | "info" | "mut" {
  if (status === "deferred" || status === "missed") return "warn";
  if (status === "due" || status === "in_progress") return "info";
  return "mut";
}

const DRAWER_ROW_LIMIT = 6;

export function LiveTrackerPassportDrawer({
  rows,
  initialSelectedGoatId,
  closeHref,
  pageContract,
}: {
  rows: LiveTrackerComboRow[];
  initialSelectedGoatId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "goat_passport",
    initialSelectedId: initialSelectedGoatId,
    closeHref,
  });
  const [passports, setPassports] = useState<Record<string, VaccinationPassport>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const requestsRef = useRef(new Map<string, ReturnType<typeof readVaccinationPassport>>());

  useEffect(() => {
    if (!displayedItem || passports[displayedItem.goat_id] || errors[displayedItem.goat_id]) return;
    const goatId = displayedItem.goat_id;
    let active = true;
    const request = requestsRef.current.get(goatId) ?? readVaccinationPassport(goatId);
    requestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setPassports((current) => ({ ...current, [goatId]: result.data }));
      else setErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, errors, passports]);

  if (!displayedItem) return null;
  const passport = passports[displayedItem.goat_id];
  const error = errors[displayedItem.goat_id];
  const open = passport?.open_obligations ?? [];
  const placeholder = copy(pageContract, "label.placeholder");

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
      <aside
        className={`drawer${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.passport.aria")}
        aria-hidden={!drawerOpen}
        inert={!drawerOpen}
      >
        <div className="dh">
          <div>
            <div className="mt">{displayedItem.display_id || displayedItem.primary_tag}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button
            ref={closeButtonRef}
            type="button"
            className="iconbtn"
            aria-label={copy(pageContract, "drawer.passport.close_label")}
            onClick={closeDrawer}
          >
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "drawer.passport.tag_1")}</div>
              <div className="v mono">{displayedItem.primary_tag || placeholder}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.passport.tag_2")}</div>
              <div className="v mono">{displayedItem.secondary_tag || placeholder}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.passport.shed")}</div>
              <div className="v">{displayedItem.shed_label || placeholder}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.passport.next_due")}</div>
              <div className="v">
                {passport?.next_due
                  ? fmtDate(passport.next_due.scheduled_for || passport.next_due.due_at)
                  : copy(pageContract, "drawer.passport.no_upcoming")}
              </div>
            </div>
          </div>

          {error ? (
            <p className="muted small" style={{ marginTop: 12 }}>
              {copy(pageContract, "drawer.passport.unavailable_prefix")}: {error}
            </p>
          ) : !passport ? (
            <p className="muted small" style={{ marginTop: 12 }} aria-live="polite">
              {copy(pageContract, "state.loading")}
            </p>
          ) : (
            <div style={{ marginTop: 14 }}>
              <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>
                {copy(pageContract, "drawer.passport.open_obligations")}
              </div>
              {open.length === 0 ? (
                <p className="muted small" style={{ margin: 0 }}>
                  {copy(pageContract, "drawer.passport.empty_open")}
                </p>
              ) : (
                <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "drawer.passport.open_obligations")}>
                  <table>
                    <tbody>
                      {open.slice(0, DRAWER_ROW_LIMIT).map((due) => (
                        <tr key={due.obligation_id}>
                          <td>{fmtDate(due.scheduled_for || due.due_at)}</td>
                          <td>{due.display_label}</td>
                          <td>
                            <Tag tone={obligationTone(due.status)}>{due.status}</Tag>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>
        <div className="df">
          <Link href={`/goats/${encodeURIComponent(displayedItem.goat_id)}`} className="btn p">
            {copy(pageContract, "drawer.passport.full_history")}
          </Link>
          <button type="button" className="btn" onClick={closeDrawer}>
            {copy(pageContract, "action.close")}
          </button>
        </div>
      </aside>
    </>
  );
}
