"use client";

// TASK WITH ITS OWN PHONE TAB (maintainer decision 2026-10-01, docs/decisions/simple-task-phone-tabs.md).
//
// The editor for a simple task authored on its module's SOP page: the tab (name, icon, list
// filters), what to do, which pens, the schedule, what to record, review, and per park the ONE
// person who does it (and, for chosen pens, which pens). Save writes a draft SOP version whose
// form_dsl carries `phone_task`; Publish puts the tab on the phone and one routine per park, in the
// backend's publish transaction. A published task is changed by saving a new version and
// publishing it, like every SOP.
//
// The schedule and what-to-record sections are the /routines drawer's own (routine-sections), so
// a phone task and a hand-made routine are authored the same way. Every vocabulary (icons, filters,
// pen scopes, cadences, review, questions, pens, people) is the backend's; every refusal is the
// backend's sentence, shown verbatim.
import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronLeft, Search } from "lucide-react";

import { AssigneePicker } from "@/components/assignee-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PenRoutineCatalog, PenRoutineKeyLabel } from "@/lib/api/pen-routines-server";
import { penKey, type ScopeKind } from "@/features/pen-routines/routine-form-model";
import { CadenceFields, ChoiceTiles, EvidenceFields, Step, toggle, type RoutineWords } from "@/features/pen-routines/routine-sections";
import { publishedHref } from "./published-href";
import { emitPhoneTask, PHONE_TASK_NAME_MAX, phoneTaskProblems, type PhoneTaskDraft, type PhoneTaskFilter, type PhoneTaskParkDraft } from "./phone-task-model";
import { publishPhoneTaskVersion, savePhoneTaskVersion, type PhoneTaskSaveResult } from "./sop-actions";
import type { SopScopeDomain } from "./sop-derive";

export type PhoneTaskParkOption = {
  parkId: string;
  name: string;
  /** That park's pens, the people who may do it there and the vocabularies; null when the read failed. */
  catalog: PenRoutineCatalog | null;
};

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  domain: SopScopeDomain;
  /** Absent for a new task: the first save creates the SOP. */
  sopId?: string;
  versionLabel?: string;
  initial: PhoneTaskDraft;
  icons: PenRoutineKeyLabel[];
  filters: PenRoutineKeyLabel[];
  parks: PhoneTaskParkOption[];
  /** The tab vocabularies could not be read. */
  loadFailed: boolean;
};

function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (hole, key: string) => (key in values ? String(values[key]) : hole));
}

export function PhoneTaskEditor({ pageContract: pc, basePath, domain, sopId: initialSopId, versionLabel, initial, icons, filters, parks, loadFailed }: Props) {
  const router = useRouter();
  const [draft, setDraft] = useState<PhoneTaskDraft>(initial);
  const [sopId, setSopId] = useState(initialSopId ?? "");
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const [penQuery, setPenQuery] = useState<Record<string, string>>({});

  const t = (key: string) => copy(pc, key);
  // The shared sections read /routines keys; on an SOP page they are the same words under `ptask.`.
  const words: RoutineWords = (key) => copy(pc, `ptask.${key}`);
  const vocabulary = useMemo(() => parks.find((park) => park.catalog)?.catalog ?? null, [parks]);
  const parkName = (parkId: string) => parks.find((park) => park.parkId === parkId)?.name ?? "";
  const problems = phoneTaskProblems(draft, parkName);
  const update = (patch: Partial<PhoneTaskDraft>) => setDraft((current) => ({ ...current, ...patch }));
  const updatePark = (parkId: string, patch: Partial<PhoneTaskParkDraft>) =>
    setDraft((current) => ({ ...current, parks: current.parks.map((park) => (park.parkId === parkId ? { ...park, ...patch } : park)) }));
  const chooseScope = (scopeKind: ScopeKind) =>
    // A whole-park task names no pens and cannot follow work done in a pen.
    setDraft((current) =>
      scopeKind === "park"
        ? { ...current, scopeKind, occupiedOnly: false, cadenceKind: current.cadenceKind === "after_work" ? "daily" : current.cadenceKind }
        : { ...current, scopeKind, occupiedOnly: current.scopeKind === "park" ? true : current.occupiedOnly },
    );
  // The SOP's name IS its code and the tab's name: fixed once the SOP exists.
  const nameLocked = Boolean(sopId);

  function submit(publish: boolean) {
    const phoneTask = emitPhoneTask(draft);
    const input = { sopId: sopId || undefined, domain, name: draft.name.trim(), phoneTask };
    startTransition(async () => {
      const res: PhoneTaskSaveResult = publish ? await publishPhoneTaskVersion(input) : await savePhoneTaskVersion(input);
      if (res.sopId && res.sopId !== sopId) {
        setSopId(res.sopId);
        // A reload now opens this SOP's editor rather than a blank new task.
        if (typeof window !== "undefined") {
          const url = new URL(window.location.href);
          url.searchParams.set("edit", res.sopId);
          window.history.replaceState(window.history.state, "", url.toString());
        }
      }
      setResult({ ok: res.ok, text: res.message || t(res.ok ? "ptask.saved" : "ptask.failed") });
      if (res.ok && publish && res.sopId) {
        router.push(publishedHref(basePath, res.sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on sop-inspection">
      <div className="phead">
        <div>
          <div className="crumb">
            {t("crumb")} · {pc.title}
            {nameLocked ? (
              <>
                {" "}
                · <b>{draft.name}</b>
              </>
            ) : null}
          </div>
          <h1>{t("ptask.title")}</h1>
          <div className="sub">{t("ptask.subtitle")}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            {versionLabel ? `${versionLabel} · ` : ""}
            {t("ptask.notice.versions")}
          </div>
        </div>
        <div className="acts">
          <Link className="btn" href={basePath}>
            <ChevronLeft className="ic" /> {t("ptask.back")}
          </Link>
        </div>
      </div>

      {loadFailed ? (
        <div className="alert" role="alert">
          <AlertTriangle className="ic" />
          <div>{t("ptask.error.load")}</div>
        </div>
      ) : null}
      {result ? (
        <div className={`alert ${result.ok ? "ok" : ""}`} role="status">
          {result.ok ? <Check className="ic" /> : <AlertTriangle className="ic" />}
          <div>{result.text}</div>
        </div>
      ) : null}

      <div className="prt ptask">
        {/* 1. The tab: its name (the SOP's name), icon, list filters, and what to do. */}
        <Step index={1} title={t("ptask.section.tab")}>
          <div className="fld">
            <label htmlFor="pt-name">{t("ptask.field.name")}</label>
            <input
              id="pt-name"
              maxLength={PHONE_TASK_NAME_MAX}
              value={draft.name}
              readOnly={nameLocked}
              disabled={nameLocked}
              onChange={(e) => update({ name: e.target.value })}
              aria-describedby="pt-name-hint"
            />
            <p className="prt-hint" id="pt-name-hint">
              {t(nameLocked ? "ptask.hint.name_fixed" : "ptask.hint.name")}
            </p>
          </div>
          <div className="prt-row2">
            <div className="fld">
              <label htmlFor="pt-icon">{t("ptask.field.icon")}</label>
              <select id="pt-icon" value={draft.icon} onChange={(e) => update({ icon: e.target.value })}>
                <option value="">{t("ptask.field.icon_choose")}</option>
                {icons.map((icon) => (
                  <option key={icon.key} value={icon.key}>
                    {icon.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="fld">
              <span className="prt-sub">{t("ptask.field.filters")}</span>
              <div className="prt-pills" role="group" aria-label={t("ptask.field.filters")}>
                {filters.map((filter) => {
                  const on = draft.filters.includes(filter.key as PhoneTaskFilter);
                  return (
                    <label key={filter.key} className={on ? "prt-pill on" : "prt-pill"}>
                      <input type="checkbox" checked={on} onChange={() => update({ filters: toggle(draft.filters, filter.key) as PhoneTaskFilter[] })} />
                      <span>{filter.label}</span>
                    </label>
                  );
                })}
              </div>
              <p className="prt-hint">{t("ptask.hint.filters")}</p>
            </div>
          </div>
          <div className="fld">
            <label htmlFor="pt-instruction">{t("ptask.field.instruction")}</label>
            <textarea id="pt-instruction" rows={2} value={draft.instruction} onChange={(e) => update({ instruction: e.target.value })} />
          </div>
        </Step>

        {/* 2. Pens: every pen, chosen pens (ticked per park below), or one task for the whole park. */}
        <Step index={2} title={t("ptask.section.pens")}>
          <ChoiceTiles name="pt-scope_kind" options={vocabulary?.scope_kinds ?? []} value={draft.scopeKind} onChange={(key) => chooseScope(key as ScopeKind)} />
          {draft.scopeKind === "all_pens" ? (
            <label className="prt-check">
              <input type="checkbox" checked={draft.occupiedOnly} onChange={(e) => update({ occupiedOnly: e.target.checked })} />
              <span>{t("ptask.field.occupied_only")}</span>
            </label>
          ) : null}
          {draft.scopeKind === "selected_pens" ? <p className="prt-hint">{t("ptask.hint.selected_pens")}</p> : null}
          {draft.scopeKind === "park" ? <p className="prt-hint">{t("ptask.hint.park_scope")}</p> : null}
        </Step>

        {/* 3. When. */}
        <Step index={3} title={t("ptask.field.cadence")}>
          <CadenceFields words={words} vocabulary={vocabulary} value={draft} onChange={update} disableAfterWork={draft.scopeKind === "park"} idPrefix="pt" />
        </Step>

        {/* 4. What to record: questions, task-wide photos / videos, the pen check-in. */}
        <Step index={4} title={t("ptask.section.capture")}>
          <EvidenceFields words={words} vocabulary={vocabulary} value={draft} onChange={update} idPrefix="pt" />
        </Step>

        {/* 5. Review: a verifier checks each submit, or none. */}
        <Step index={5} title={t("ptask.field.review")}>
          <ChoiceTiles name="pt-review_kind" options={vocabulary?.review_kinds ?? []} value={draft.reviewKind} onChange={(key) => update({ reviewKind: key as PhoneTaskDraft["reviewKind"] })} />
        </Step>

        {/* 6. Parks: where it runs, the ONE person who does it there, and the pens when pens are chosen. */}
        <Step index={6} title={t("ptask.section.parks")} hint={t("ptask.hint.parks")}>
          {draft.parks.map((park) => {
            const option = parks.find((item) => item.parkId === park.parkId);
            const catalog = option?.catalog ?? null;
            const people = catalog?.people ?? [];
            const owners = people.map((person) => ({ id: person.user_id, name: person.display_name, title: person.title }));
            const assigneeGone = Boolean(park.assigneeUserId) && catalog !== null && !people.some((person) => person.user_id === park.assigneeUserId);
            const pens = catalog?.pens ?? [];
            const needle = (penQuery[park.parkId] ?? "").trim().toLowerCase();
            const shownPens = needle ? pens.filter((pen) => pen.operational_location_display.toLowerCase().includes(needle)) : pens;
            return (
              <div key={park.parkId} className="prt-question ptask-park">
                <label className="prt-check">
                  <input type="checkbox" checked={park.included} onChange={(e) => updatePark(park.parkId, { included: e.target.checked })} />
                  <span>
                    <b>{option?.name ?? park.parkId}</b>
                  </span>
                </label>
                {park.included ? (
                  <>
                    {!catalog ? <p className="prt-hint prt-warn">{t("ptask.error.catalog")}</p> : null}
                    <div className="fld">
                      <span className="prt-sub">{t("ptask.field.assignee")}</span>
                      <AssigneePicker
                        mode="single"
                        labels={{
                          label: t("ptask.field.assignee"),
                          search: t("ptask.assignee.search"),
                          none: t("ptask.assignee.none"),
                          placeholder: t("ptask.assignee.placeholder"),
                        }}
                        owners={owners}
                        selected={park.assigneeUserId || undefined}
                        onSelect={(next) => updatePark(park.parkId, { assigneeUserId: next ?? "" })}
                      />
                      {assigneeGone ? (
                        <p className="prt-hint prt-warn" role="status">
                          {t("ptask.assignee.unavailable")}
                        </p>
                      ) : null}
                      {catalog && !owners.length ? <p className="prt-hint prt-warn">{t("ptask.empty.people")}</p> : null}
                    </div>
                    {draft.scopeKind === "selected_pens" ? (
                      <div className="prt-pens">
                        <span className="prt-sub">{t("ptask.field.pens")}</span>
                        <div className="prt-pens-bar">
                          <span className="prt-search">
                            <Search className="ic" aria-hidden="true" />
                            <input
                              type="search"
                              value={penQuery[park.parkId] ?? ""}
                              placeholder={t("ptask.filter.pens_search")}
                              aria-label={t("ptask.filter.pens_search")}
                              onChange={(e) => setPenQuery((current) => ({ ...current, [park.parkId]: e.target.value }))}
                            />
                          </span>
                          <button type="button" className="btn sm" disabled={!shownPens.length} onClick={() => updatePark(park.parkId, { pens: [...new Set([...park.pens, ...shownPens.map(penKey)])] })}>
                            {t("ptask.action.select_all_pens")}
                          </button>
                          <button
                            type="button"
                            className="btn sm"
                            disabled={!park.pens.length}
                            onClick={() => updatePark(park.parkId, { pens: park.pens.filter((key) => !shownPens.some((pen) => penKey(pen) === key)) })}
                          >
                            {t("ptask.action.clear_pens")}
                          </button>
                          <span className="prt-count">{fill(t("ptask.count.pens_chosen"), { chosen: park.pens.length, total: pens.length })}</span>
                        </div>
                        <div className="prt-pen-list">
                          {shownPens.map((pen) => {
                            const key = penKey(pen);
                            const on = park.pens.includes(key);
                            return (
                              <label key={key} className={["prt-check", "prt-pen", on ? "on" : "", pen.occupied ? "" : "empty"].filter(Boolean).join(" ")}>
                                <input type="checkbox" checked={on} onChange={() => updatePark(park.parkId, { pens: toggle(park.pens, key) })} />
                                {/* Backend-composed pen label, rendered verbatim: "Castro 2", "Godel 1 - Part 3". */}
                                <span>{pen.operational_location_display}</span>
                                {pen.occupied ? null : <span className="prt-count">{t("ptask.label.pen_empty")}</span>}
                              </label>
                            );
                          })}
                          {!shownPens.length ? <p className="prt-hint">{t("ptask.empty.pens_search")}</p> : null}
                        </div>
                      </div>
                    ) : null}
                  </>
                ) : null}
              </div>
            );
          })}
        </Step>
      </div>

      <div className="cfgmf inspection-footer">
        <div>
          {problems.length ? (
            <ul className="small muted" style={{ margin: "4px 0 0 16px" }}>
              {problems.slice(0, 5).map((problem, i) => (
                <li key={i}>{fill(t(problem.key), { park: problem.park ?? "" })}</li>
              ))}
            </ul>
          ) : (
            <span className="muted small">{t("ptask.footer.ready")}</span>
          )}
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {t("ptask.action.save_draft")}
        </button>
        <button type="button" className="btn p" disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          <Check className="ic" /> {t("ptask.action.publish")}
        </button>
      </div>
    </div>
  );
}
