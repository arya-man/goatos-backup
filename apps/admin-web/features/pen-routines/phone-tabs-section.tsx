"use client";

import { Smartphone } from "lucide-react";
import { startTransition, useActionState, useEffect, useMemo, useRef, useState } from "react";

import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { currentHistoryEntryIsLocalOverlay, LocalOverlayLink, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PenRoutineKeyLabel, PenRoutineTab } from "@/lib/api/pen-routines-server";
import { createPhoneTabAction, setPhoneTabStatusAction, updatePhoneTabAction, type PhoneTabActionState } from "./phone-tab-actions";
import { applySavedTab, routinesByPark, TAB_FORM_JSON_FIELDS, TAB_LABEL_MAX } from "./phone-tab-model";
import { RoutineSaveFooter, SaveStateBridge } from "./routine-drawer";

/**
 * PHONE TABS on /routines (maintainer instruction 2026-10-01, docs/decisions/simple-task-phone-tabs.md).
 * A simple task -- "fumigation: every pen, two videos, the verifier approves" -- is a routine, and a
 * phone tab is WHERE it appears on the phone: its own bottom-bar item, with a name, inside a chosen
 * phone module, with an icon from a closed set and the list filters the phone offers. Adding one
 * needs no code: it is written here and the phone composes the item from the bootstrap.
 *
 * Everything the reader sees is backend-owned: the tabs and their module labels from the tabs read,
 * the module / icon / filter vocabularies from the same read (closed sets, in the backend's order),
 * the routines from the routines read, and every fixed word from the page contract.
 *
 * Writes are capability-gated on the page contract's routine controls (pen_routines.configure is
 * the one authority behind routines and tabs alike) AND on the route table behind each action.
 *
 * The drawer is client-local (`?tab=new` / `?tab=<tab_id>` through LocalOverlayLink), and a save
 * RETURNS the tab: this section applies it to its own list in place -- a routine ticked here leaves
 * whichever tab it was on, exactly as the backend moved it -- so nothing re-reads the page.
 */

export const PARAM_TAB = "tab";

export type PhoneTabsRoutine = { routine_id: string; name: string; park_id: string; park_name: string; status: string };

export type PhoneTabsVocabulary = {
  modules: PenRoutineKeyLabel[];
  icons: PenRoutineKeyLabel[];
  filters: PenRoutineKeyLabel[];
};

const INITIAL: PhoneTabActionState = { status: "idle", code: "", detail: "", tab: null, ticket: 0 };

/** `base` with the tab drawer's parameter set; `base` already carries no drawer parameter. */
function withTab(base: string, id: string): string {
  const url = new URL(base, "http://x");
  url.searchParams.set(PARAM_TAB, id);
  return `${url.pathname}${url.search}`;
}

/** Closes the overlay the same way the drawer's own X does: Back when the entry is local, else replace. */
function closeOverlay(closeHref: string): void {
  if (currentHistoryEntryIsLocalOverlay()) {
    window.history.back();
    return;
  }
  replaceLocalOverlayUrl(closeHref);
}

function labelFor(options: PenRoutineKeyLabel[], key: string): string {
  return options.find((option) => option.key === key)?.label ?? "";
}

type Draft = { label: string; moduleKey: string; iconKey: string; filters: string[]; routineIds: string[] };

function draftFrom(tab: PenRoutineTab | undefined): Draft {
  if (!tab) return { label: "", moduleKey: "", iconKey: "", filters: [], routineIds: [] };
  return {
    label: tab.label,
    moduleKey: tab.module_key,
    iconKey: tab.icon_key,
    filters: [...tab.filters],
    routineIds: tab.routines.map((routine) => routine.routine_id),
  };
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value];
}

function PhoneTabForm({
  pageContract,
  tab,
  vocabulary,
  routines,
  tabNameByRoutine,
  canSave,
  canSetStatus,
  closeHref,
  formId,
  onSaved,
}: {
  pageContract: AdminUiPageContract;
  /** Absent for the create form. */
  tab?: PenRoutineTab;
  vocabulary: PhoneTabsVocabulary;
  routines: PhoneTabsRoutine[];
  /** Which tab each routine sits on now (by routine id), so the editor can say a tick moves it. */
  tabNameByRoutine: Map<string, { tabId: string; label: string }>;
  canSave: boolean;
  canSetStatus: boolean;
  closeHref: string;
  formId: string;
  onSaved: (tab: PenRoutineTab) => void;
}) {
  const isEdit = tab !== undefined;
  const c = (key: string) => copy(pageContract, key);
  const [draft, setDraft] = useState<Draft>(() => draftFrom(tab));
  const [state, formAction, pending] = useActionState(isEdit ? updatePhoneTabAction : createPhoneTabAction, INITIAL);
  const [statusState, statusFormAction, statusPending] = useActionState(setPhoneTabStatusAction, INITIAL);
  const [confirmRetire, setConfirmRetire] = useState(false);

  // A successful save or status change applies the returned tab in place and closes the drawer.
  const applied = useRef(0);
  useEffect(() => {
    const latest = [state, statusState].find((item) => item.status === "success" && item.tab);
    if (!latest || !latest.tab) return;
    const ticket = state.ticket * 1000 + statusState.ticket;
    if (applied.current === ticket) return;
    applied.current = ticket;
    onSaved(latest.tab);
    closeOverlay(closeHref);
  }, [state, statusState, onSaved, closeHref]);

  const update = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));
  const outcome = (item: PhoneTabActionState) =>
    item.status === "idle" ? "" : item.detail || c(`tabs.action.${item.code}`);
  const message = outcome(state);
  const statusMessage = outcome(statusState);
  const readOnly = !canSave;
  const keep = useMemo(() => new Set(draft.routineIds), [draft.routineIds]);
  const groups = useMemo(() => routinesByPark(routines, keep), [routines, keep]);
  const chosenCount = draft.routineIds.length;

  return (
    <div className="prt">
      <form
        id={formId}
        aria-busy={pending}
        className="prt-form"
        onSubmit={(event) => {
          // Through a transition rather than `action=`, so a refused save leaves the typed fields as they were.
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          startTransition(() => formAction(data));
        }}
      >
        {isEdit ? <input type="hidden" name="tab_id" value={tab.tab_id} /> : null}
        {isEdit ? <input type="hidden" name="row_version" value={tab.row_version} /> : null}
        <input type="hidden" name={TAB_FORM_JSON_FIELDS.filters} value={JSON.stringify(draft.filters)} />
        <input type="hidden" name={TAB_FORM_JSON_FIELDS.routineIds} value={JSON.stringify(draft.routineIds)} />
        {readOnly ? <div className="note">{c("configure.disabled_no_access")}</div> : null}

        <fieldset disabled={readOnly} className="prt-fieldset">
          <section className="prt-step" aria-label={c("tabs.section.where")}>
            <header className="prt-step-hd">
              <h4>{c("tabs.section.where")}</h4>
            </header>
            <div className="fld">
              <label htmlFor={`${formId}-label`}>{c("tabs.field.label")}</label>
              <input
                id={`${formId}-label`}
                name="label"
                required
                maxLength={TAB_LABEL_MAX}
                value={draft.label}
                onChange={(event) => update({ label: event.target.value })}
              />
              <p className="prt-hint">
                {c("tabs.hint.label")} <span className="prt-count">{`${draft.label.length}/${TAB_LABEL_MAX}`}</span>
              </p>
            </div>
            <div className="prt-row2">
              <div className="fld">
                <label htmlFor={`${formId}-module`}>{c("tabs.field.module")}</label>
                <select id={`${formId}-module`} name="module_key" required value={draft.moduleKey} onChange={(event) => update({ moduleKey: event.target.value })}>
                  <option value="" disabled>
                    {c("tabs.field.choose")}
                  </option>
                  {vocabulary.modules.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor={`${formId}-icon`}>{c("tabs.field.icon")}</label>
                {/* The icon set is a closed backend vocabulary the phone draws; the web shows its names. */}
                <select id={`${formId}-icon`} name="icon_key" required value={draft.iconKey} onChange={(event) => update({ iconKey: event.target.value })}>
                  <option value="" disabled>
                    {c("tabs.field.choose")}
                  </option>
                  {vocabulary.icons.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <p className="prt-hint">{c("tabs.hint.module")}</p>
          </section>

          <section className="prt-step" aria-label={c("tabs.field.filters")}>
            <header className="prt-step-hd">
              <h4>{c("tabs.field.filters")}</h4>
            </header>
            <p className="prt-hint">{c("tabs.hint.filters")}</p>
            <div className="prt-pills">
              {vocabulary.filters.map((option) => {
                const on = draft.filters.includes(option.key);
                return (
                  <label key={option.key} className={on ? "prt-pill on" : "prt-pill"}>
                    <input type="checkbox" checked={on} onChange={() => update({ filters: toggle(draft.filters, option.key) })} />
                    <span>{option.label}</span>
                  </label>
                );
              })}
            </div>
          </section>

          <section className="prt-step" aria-label={c("tabs.field.routines")}>
            <header className="prt-step-hd">
              <h4>{c("tabs.field.routines")}</h4>
              <span className="prt-step-aside">{chosenCount}</span>
            </header>
            <p className="prt-hint">{c("tabs.hint.routines")}</p>
            {groups.length === 0 ? <p className="prt-hint prt-warn">{c("tabs.empty.routines_pick")}</p> : null}
            {groups.map((group) => (
              <div key={group.parkId} className="prt-tab-park">
                <span className="prt-sub">{group.parkName}</span>
                <div className="prt-tab-routines">
                  {group.routines.map((routine) => {
                    const on = draft.routineIds.includes(routine.routine_id);
                    const elsewhere = tabNameByRoutine.get(routine.routine_id);
                    const onAnother = elsewhere && elsewhere.tabId !== tab?.tab_id ? elsewhere.label : "";
                    return (
                      <label key={routine.routine_id} className="prt-check">
                        <input type="checkbox" checked={on} onChange={() => update({ routineIds: toggle(draft.routineIds, routine.routine_id) })} />
                        <span>{routine.name}</span>
                        {onAnother ? <span className="prt-count">{`${c("tabs.label.on_tab")} ${onAnother}`}</span> : null}
                      </label>
                    );
                  })}
                </div>
              </div>
            ))}
          </section>
        </fieldset>

        {message && state.status === "error" ? (
          <div className="prt-outcome error" role="alert">
            {message}
          </div>
        ) : null}
      </form>

      {/* Retire / restore. Retiring takes the tab off every phone; its routines stay on it and keep
          running, so restoring puts it back exactly as it was. Retire asks once -- in place, as two
          buttons, never the browser's own box. */}
      {isEdit && canSetStatus ? (
        <form action={statusFormAction} aria-busy={statusPending} className="prt-status">
          <input type="hidden" name="tab_id" value={tab.tab_id} />
          <input type="hidden" name="row_version" value={tab.row_version} />
          <Tag tone={tab.status === "active" ? "ok" : "mut"}>{c(`tabs.status.${tab.status}`)}</Tag>
          {tab.status === "retired" ? (
            <button key="tab-restore" type="submit" name="status" value="active" className="btn sm" disabled={statusPending}>
              {c("tabs.action.restore")}
            </button>
          ) : confirmRetire ? (
            <>
              <span className="prt-confirm">{c("tabs.action.retire.confirm")}</span>
              <button key="tab-retire-confirm" type="submit" name="status" value="retired" className="btn sm danger" disabled={statusPending}>
                {c("tabs.action.retire")}
              </button>
              <button key="tab-retire-keep" type="button" className="btn sm" onClick={() => setConfirmRetire(false)} disabled={statusPending}>
                {c("tabs.action.retire.keep")}
              </button>
            </>
          ) : (
            // Distinct keys: React must not reuse this node as the confirm's submit button mid-click.
            <button key="tab-retire-ask" type="button" className="btn sm" onClick={() => setConfirmRetire(true)} disabled={statusPending}>
              {c("tabs.action.retire")}
            </button>
          )}
          {statusMessage ? (
            <span role="status" className={statusState.status === "success" ? "prt-outcome ok" : "prt-outcome error"}>
              {statusMessage}
            </span>
          ) : null}
        </form>
      ) : null}

      <SaveStateBridge formId={formId} pending={pending} message={message} tone={state.status} />
    </div>
  );
}

export function PhoneTabsSection({
  pageContract,
  initialTabs,
  vocabulary,
  loadError,
  routines,
  canCreate,
  canEdit,
  canSetStatus,
  closeHref,
  initialSelectedId,
}: {
  pageContract: AdminUiPageContract;
  initialTabs: PenRoutineTab[];
  vocabulary: PhoneTabsVocabulary;
  /** The backend's own message when the tabs read failed; empty otherwise. */
  loadError: string;
  /** Every park's routines (the picker's choices), slimmed to what the picker shows. */
  routines: PhoneTabsRoutine[];
  canCreate: boolean;
  canEdit: boolean;
  canSetStatus: boolean;
  /** The page URL with neither drawer open. */
  closeHref: string;
  initialSelectedId?: string;
}) {
  const c = (key: string) => copy(pageContract, key);
  const [tabs, setTabs] = useState<PenRoutineTab[]>(initialTabs);
  // A fresh server read (another park chosen, a reload) replaces what this section holds.
  const [seen, setSeen] = useState(initialTabs);
  if (seen !== initialTabs) {
    setSeen(initialTabs);
    setTabs(initialTabs);
  }
  const onSaved = useMemo(() => (saved: PenRoutineTab) => setTabs((current) => applySavedTab(current, saved)), []);

  const tabNameByRoutine = useMemo(() => {
    const map = new Map<string, { tabId: string; label: string }>();
    for (const tab of tabs) for (const routine of tab.routines) map.set(routine.routine_id, { tabId: tab.tab_id, label: tab.label });
    return map;
  }, [tabs]);

  const items = useMemo(() => {
    const list: LocalOverlayDrawerItem[] = [];
    const icon = <Smartphone className="ic" aria-hidden="true" />;
    if (canCreate) {
      list.push({
        id: "new",
        eyebrow: copy(pageContract, "tabs.drawer.title"),
        title: copy(pageContract, "tabs.drawer.create_title"),
        icon,
        body: (
          <PhoneTabForm
            key="new"
            pageContract={pageContract}
            vocabulary={vocabulary}
            routines={routines}
            tabNameByRoutine={tabNameByRoutine}
            canSave={canCreate}
            canSetStatus={false}
            closeHref={closeHref}
            formId="prt-tab-form-new"
            onSaved={onSaved}
          />
        ),
        footer: <RoutineSaveFooter key="save" formId="prt-tab-form-new" saveLabel={copy(pageContract, "tabs.action.save")} canSave={canCreate} />,
      });
    }
    for (const tab of tabs) {
      const formId = `prt-tab-form-${tab.tab_id}`;
      list.push({
        id: tab.tab_id,
        eyebrow: copy(pageContract, "tabs.drawer.title"),
        title: canEdit ? copy(pageContract, "tabs.drawer.edit_title") : tab.label,
        icon,
        body: (
          <PhoneTabForm
            // A saved version remounts the form, so reopening shows what was saved.
            key={`${tab.tab_id}:${tab.row_version}`}
            pageContract={pageContract}
            tab={tab}
            vocabulary={vocabulary}
            routines={routines}
            tabNameByRoutine={tabNameByRoutine}
            canSave={canEdit}
            canSetStatus={canSetStatus}
            closeHref={closeHref}
            formId={formId}
            onSaved={onSaved}
          />
        ),
        footer: <RoutineSaveFooter key="save" formId={formId} saveLabel={copy(pageContract, "tabs.action.save")} canSave={canEdit} />,
      });
    }
    return list;
  }, [tabs, pageContract, vocabulary, routines, tabNameByRoutine, canCreate, canEdit, canSetStatus, closeHref, onSaved]);

  const filterLabels = (tab: PenRoutineTab) => tab.filters.map((key) => labelFor(vocabulary.filters, key)).filter(Boolean).join(", ");
  const routineNames = (tab: PenRoutineTab) => tab.routines.map((routine) => `${routine.name} · ${routine.park_name}`).join(", ");
  const cell = (tab: PenRoutineTab, key: string) => {
    switch (key) {
      case "tab":
        return (
          <span className="prt-people">
            <LocalOverlayLink href={withTab(closeHref, tab.tab_id)} scroll={false} className="prt-name-link">
              {tab.label}
            </LocalOverlayLink>
            <span className="muted small">{labelFor(vocabulary.icons, tab.icon_key)}</span>
          </span>
        );
      case "module":
        return tab.module_label;
      case "filters":
        return filterLabels(tab) || c("label.placeholder");
      case "routines":
        return tab.routines.length ? routineNames(tab) : <span className="prt-warn">{c("tabs.empty.tab_routines")}</span>;
      case "status":
        return <Tag tone={tab.status === "active" ? "ok" : "mut"}>{c(`tabs.status.${tab.status}`)}</Tag>;
      default:
        return null;
    }
  };
  const columns = ["tab", "module", "filters", "routines", "status"].map((key) => ({ key, label: c(`tabs.column.${key}`) }));
  const title = c("tabs.title");

  return (
    <>
      <section className="card" aria-label={title}>
        <div className="hd">
          <Smartphone className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{title}</h3>
          <div className="sp" style={{ flex: 1 }} />
          {canCreate ? (
            <LocalOverlayLink href={withTab(closeHref, "new")} scroll={false} className="btn sm primary">
              {c("tabs.action.create")}
            </LocalOverlayLink>
          ) : null}
        </div>
        <div className="bd">
          <p className="prt-hint" style={{ margin: "10px 0 0" }}>
            {c("tabs.subtitle")}
          </p>
          {loadError ? (
            <div className="alert" role="alert">
              {loadError}
            </div>
          ) : tabs.length === 0 ? (
            <div className="empty">{c("tabs.empty")}</div>
          ) : (
            <>
              {/* Phone: one card per tab, the same cells, so nothing sits past the right edge. */}
              <ul className="prt-cards" aria-label={title}>
                {tabs.map((tab) => (
                  <li key={tab.tab_id} className="prt-card">
                    <div className="prt-card-hd">
                      <span className="prt-card-name">{cell(tab, "tab")}</span>
                      {cell(tab, "status")}
                    </div>
                    <dl>
                      {columns
                        .filter((column) => column.key !== "tab" && column.key !== "status")
                        .map((column) => (
                          <div key={column.key}>
                            <dt>{column.label}</dt>
                            <dd>{cell(tab, column.key)}</dd>
                          </div>
                        ))}
                    </dl>
                  </li>
                ))}
              </ul>
              <div className="tablewrap prt-table" tabIndex={0} role="group" aria-label={title}>
                <table className="tbl">
                  <thead>
                    <tr>
                      {columns.map((column) => (
                        <th key={column.key}>{column.label}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {tabs.map((tab) => (
                      <tr key={tab.tab_id}>
                        {columns.map((column) => (
                          <td key={column.key} className={`prt-col-tab-${column.key}`}>
                            {cell(tab, column.key)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      </section>
      <LocalOverlayDrawer
        items={items}
        selectionKey={PARAM_TAB}
        initialSelectedId={initialSelectedId}
        closeHref={closeHref}
        ariaLabel={c("tabs.drawer.title")}
        closeLabel={c("action.close")}
      />
    </>
  );
}
