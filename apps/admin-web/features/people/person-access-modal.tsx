"use client";

import { AlertTriangle, ShieldCheck, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";

import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AccessModuleWrite, PersonAccess } from "@/lib/api/server";
import { designationDefaultsAction, savePersonAccessAction } from "./access-actions";

/**
 * The per-person access editor.
 *
 * One question per row: what may this person do with this module, on the web
 * console and on the phone. Every visible word — module names, capability names,
 * the blurbs, the warning — comes from `access`, which the backend composed. The
 * raw vocabulary behind these chips is `aas_health` and `oversee`, and neither is
 * a word to put in front of someone deciding what a colleague may do.
 *
 * Local state only until Save. Toggling a chip must not navigate or re-render the
 * page beneath (the local-overlay contract), so every control here is a button.
 */

type Draft = {
  designationCode: string;
  scopeMode: "tenant" | "parks";
  parkIDs: string[];
  /** The one park per-park work is assigned in; asked only when more than one park is ticked. */
  homeParkID: string;
  /** module_key -> surface -> capability levels, plus the web page ticks */
  modules: Record<string, { web: string[]; mobile: string[]; pages: string[] }>;
};

function draftFrom(access: PersonAccess): Draft {
  const modules: Draft["modules"] = {};
  for (const row of access.modules) {
    modules[row.module_key] = {
      web: [...row.granted_web],
      mobile: [...row.granted_mobile],
      pages: [...row.granted_pages_web],
    };
  }
  return {
    designationCode: access.designation_code ?? "",
    scopeMode: access.scope_mode === "tenant" ? "tenant" : "parks",
    parkIDs: [...access.park_ids],
    homeParkID: access.home_park_id ?? "",
    modules,
  };
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

export function PersonAccessModal({
  access,
  pageContract,
  onClose,
}: {
  access: PersonAccess;
  pageContract: AdminUiPageContract;
  onClose: () => void;
}) {
  // Capability-gated, never a role check in the component (AGENTS.md, the 2026-08-12 STG
  // incident). The PUT route requires the same permission, so this is the honest label
  // rather than the lock.
  const mayEdit = controlEnabled(pageContract, "edit_access", false);
  const cannotEditReason = control(pageContract, "edit_access").disabled_reason;
  const t = (key: string) => copy(pageContract, key);
  const [draft, setDraft] = useState<Draft>(() => draftFrom(access));
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const closeRef = useRef<HTMLButtonElement>(null);

  // No re-seed effect: the launcher mounts this only once it has the record and
  // discards the record on close, so the useState initialiser above is the only seed
  // that can run. A successful save closes the editor, and a failed one deliberately
  // keeps what the admin typed so they can correct one tick rather than start again.

  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  // Escape closes, matching every other overlay in this system.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const setPage = useCallback((moduleKey: string, pageKey: string) => {
    setDraft((current) => {
      const row = current.modules[moduleKey] ?? { web: [], mobile: [], pages: [] };
      return {
        ...current,
        modules: { ...current.modules, [moduleKey]: { ...row, pages: toggle(row.pages, pageKey) } },
      };
    });
  }, []);

  const setCapability = useCallback((moduleKey: string, surface: "web" | "mobile", level: string) => {
    setDraft((current) => {
      const row = current.modules[moduleKey] ?? { web: [], mobile: [], pages: [] };
      const next = { ...row, [surface]: toggle(row[surface], level) };
      // Granting a module for the first time opens it on every screen it has. That is
      // what the backend does with an empty stored list, and the alternative -- a
      // module ticked with no screens -- is refused on save rather than silently
      // widened, so the editor must never leave it in that state.
      if (surface === "web" && next.web.length > 0 && next.pages.length === 0) {
        const catalog = access.modules.find((m) => m.module_key === moduleKey);
        next.pages = (catalog?.pages ?? []).map((page) => page.page_key);
      }
      return { ...current, modules: { ...current.modules, [moduleKey]: next } };
    });
  }, [access.modules]);

  const applyDesignation = useCallback(
    (code: string) => {
      setDraft((current) => ({ ...current, designationCode: code }));
      if (!code) return;
      // Pre-fill from the designation's defaults. It REPLACES the module ticks
      // rather than merging: "start this person from Feed Director" is the whole
      // point, and merging would leave whatever was there before silently on.
      startTransition(async () => {
        const result = await designationDefaultsAction(code);
        if (!result.ok) {
          setError(t("access.error.defaults"));
          return;
        }
        setDraft((current) => {
          // Every rendered module is reset first, so applying a designation is a
          // clean start rather than a merge over whatever happened to be ticked.
          const modules: Draft["modules"] = {};
          for (const row of access.modules) modules[row.module_key] = { web: [], mobile: [], pages: [] };
          for (const row of result.modules) {
            modules[row.module_key] = {
              web: [...row.web],
              mobile: [...row.mobile],
              pages: [...(row.pages ?? [])],
            };
          }
          return { ...current, modules };
        });
      });
    },
    [access.modules],
  );

  const grantedCount = useMemo(
    () =>
      Object.values(draft.modules).filter((row) => row.web.length > 0 || row.mobile.length > 0).length,
    [draft.modules],
  );

  const capabilityLabel = useCallback(
    (level: string) => access.capabilities.find((c) => c.level === level),
    [access.capabilities],
  );

  const submit = useCallback(() => {
    setError("");
    const modules: AccessModuleWrite[] = access.modules.map((row) => ({
      module_key: row.module_key,
      web: draft.modules[row.module_key]?.web ?? [],
      mobile: draft.modules[row.module_key]?.mobile ?? [],
      pages: draft.modules[row.module_key]?.pages ?? [],
    }));
    startTransition(async () => {
      const result = await savePersonAccessAction({
        personId: access.person_id,
        body: {
          designation_code: draft.designationCode,
          scope_mode: draft.scopeMode,
          park_ids: draft.scopeMode === "parks" ? draft.parkIDs : [],
          // One ticked park IS the home park; the select only exists past one. The
          // backend validates the same rule, so this is the honest value, not the lock.
          home_park_id:
            draft.scopeMode === "parks" && draft.parkIDs.length === 1 ? draft.parkIDs[0] : draft.homeParkID,
          modules,
          row_version: access.row_version,
        },
      });
      if (result.ok) {
        onClose();
        return;
      }
      // Backend-composed copy, rendered verbatim: it names WHICH tick was refused.
      setError(result.message || t("access.error.save"));
    });
  }, [access.modules, access.person_id, access.row_version, draft, onClose]);

  return (
    <div className="vr-modal-scrim on" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div
        className="vr-modal on"
        role="dialog"
        aria-modal="true"
        aria-label={`Access for ${access.display_name}`}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="vr-modal-hd">
          <ShieldCheck size={20} aria-hidden />
          <div>
            <h2>
              {t("access.title")} · {access.display_name}
            </h2>
            <div className="sb">
              {access.email ? `${access.email} · ` : ""}
              {grantedCount === 0
                ? t("access.summary.none")
                : `${grantedCount} ${t("access.summary.count").replace("{total}", String(access.modules.length))}`}
            </div>
          </div>
          <button ref={closeRef} type="button" className="x" onClick={onClose} aria-label={t("action.close")}>
            <X size={18} aria-hidden />
          </button>
        </div>

        <div className="vr-modal-bd">
          {error ? (
            <div className="alert" role="alert">
              {error}
            </div>
          ) : null}

          {access.warnings.map((warning) => (
            <div key={warning.module_key} className="pa-warn">
              <AlertTriangle size={16} aria-hidden style={{ flexShrink: 0, marginTop: 1 }} />
              <div>
                <b>{t("access.warning.title")}</b>
                {warning.message}
              </div>
            </div>
          ))}

          <div className="pa-setup">
            <div className="fld">
              <label htmlFor="pa-designation" className="pa-lbl">
                {t("access.designation")}
              </label>
              <select
                id="pa-designation"
                value={draft.designationCode}
                disabled={pending || !mayEdit}
                onChange={(event) => applyDesignation(event.target.value)}
              >
                <option value="">{t("access.designation.none")}</option>
                {access.designations.map((designation) => (
                  <option key={designation.code} value={designation.code}>
                    {designation.label}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <div className="pa-lbl">{t("access.scope")}</div>
              <div className="pa-pills">
                <button
                  type="button"
                  className={`pa-pill${draft.scopeMode === "tenant" ? " on" : ""}`}
                  aria-pressed={draft.scopeMode === "tenant"}
                  onClick={() => setDraft((c) => ({ ...c, scopeMode: "tenant" }))}
                >
                  {t("access.scope.tenant")}
                </button>
                {access.parks.map((park) => {
                  const on = draft.scopeMode === "parks" && draft.parkIDs.includes(park.park_id);
                  return (
                    <button
                      key={park.park_id}
                      type="button"
                      className={`pa-pill${on ? " on" : ""}`}
                      aria-pressed={on}
                      onClick={() =>
                        setDraft((c) => {
                          const parkIDs = toggle(c.scopeMode === "parks" ? c.parkIDs : [], park.park_id);
                          // An unticked park cannot stay the home park.
                          const homeParkID = parkIDs.includes(c.homeParkID) ? c.homeParkID : "";
                          return { ...c, scopeMode: "parks", parkIDs, homeParkID };
                        })
                      }
                    >
                      {park.label}
                    </button>
                  );
                })}
              </div>
            </div>

            {draft.scopeMode === "parks" && draft.parkIDs.length > 1 ? (
              <div className="fld">
                <label className="pa-lbl" htmlFor="pa-home-park">
                  {t("access.home_park")}
                </label>
                <select
                  id="pa-home-park"
                  value={draft.homeParkID}
                  disabled={!mayEdit || pending}
                  onChange={(event) => {
                    const homeParkID = event.target.value;
                    setDraft((c) => ({ ...c, homeParkID }));
                  }}
                >
                  <option value="">{t("access.home_park.none")}</option>
                  {access.parks
                    .filter((park) => draft.parkIDs.includes(park.park_id))
                    .map((park) => (
                      <option key={park.park_id} value={park.park_id}>
                        {park.label}
                      </option>
                    ))}
                </select>
                <div className="pa-na">{t("access.home_park.hint")}</div>
              </div>
            ) : null}
          </div>

          <div className="pa-gridwrap">
            <table className="pa-grid">
              <thead>
                <tr>
                  <th>{t("access.column.module")}</th>
                  <th className="pa-surface">{t("access.column.web")}</th>
                  <th className="pa-surface">{t("access.column.mobile")}</th>
                </tr>
              </thead>
              <tbody>
                {access.modules.map((row) => {
                  const held = draft.modules[row.module_key] ?? { web: [], mobile: [], pages: [] };
                  const hasAny = held.web.length > 0 || held.mobile.length > 0;
                  return (
                    <tr key={row.module_key} className={hasAny ? "pa-has" : undefined}>
                      <td className="pa-mod">
                        <b>{row.label}</b>
                        <span>{row.blurb}</span>
                      </td>
                      {(["web", "mobile"] as const).map((surface) => {
                        const offered = surface === "web" ? row.offered_web : row.offered_mobile;
                        if (offered.length === 0) {
                          return (
                            <td key={surface}>
                              <span className="pa-na">
                                {t(surface === "web" ? "access.unavailable.web" : "access.unavailable.mobile")}
                              </span>
                            </td>
                          );
                        }
                        // Page ticks belong to the WEB cell alone and only once the
                        // module is granted: which screens someone keeps is a question
                        // that only exists after they have the module at all.
                        const showPages =
                          surface === "web" && row.pages.length > 0 && held.web.length > 0;
                        return (
                          <td key={surface}>
                            <div className="pa-caps">
                              {offered.map((level) => {
                                const copy = capabilityLabel(level);
                                const on = held[surface].includes(level);
                                return (
                                  <button
                                    key={level}
                                    type="button"
                                    className={`pa-cap${on ? " on" : ""}`}
                                    aria-pressed={on}
                                    title={copy?.blurb}
                                    disabled={pending || !mayEdit}
                                    onClick={() => setCapability(row.module_key, surface, level)}
                                  >
                                    {copy?.label ?? level}
                                  </button>
                                );
                              })}
                            </div>
                            {showPages ? (
                              <div className="pa-pages" role="group" aria-label={t("access.pages.label")}>
                                {row.pages.map((page) => {
                                  const on = held.pages.includes(page.page_key);
                                  return (
                                    <button
                                      key={page.page_key}
                                      type="button"
                                      className={`pa-page${on ? " on" : ""}`}
                                      aria-pressed={on}
                                      disabled={pending || !mayEdit}
                                      onClick={() => setPage(row.module_key, page.page_key)}
                                    >
                                      {page.label}
                                    </button>
                                  );
                                })}
                              </div>
                            ) : null}
                          </td>
                        );
                      })}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>

        <div className="df" style={{ display: "flex", gap: 9, justifyContent: "flex-end" }}>
          <button type="button" className="btn ghost" onClick={onClose} disabled={pending}>
            {t("action.cancel")}
          </button>
          <button
            type="button"
            className="btn p"
            onClick={submit}
            disabled={pending || !mayEdit}
            title={mayEdit ? undefined : cannotEditReason}
            aria-disabled={!mayEdit}
          >
            {pending ? t("access.action.saving") : t("access.action.save")}
          </button>
        </div>
      </div>
    </div>
  );
}
