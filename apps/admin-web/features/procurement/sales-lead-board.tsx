"use client";

// One searchable, paged, editable pipeline board — the buyer leads or the farmer groups — as it
// appears inside the Sales Config pipeline drawer.
//
// Why this exists: the drawer used to list the NEWEST TWENTY leads with no search and no pager,
// so 188 of the 208 buyers could not be reached at all, and no lead carried a phone number, so the
// list it did show could not actually be called. This board answers the whole pipeline: a staged
// search + call-status facet, a pager over the backend's WHOLE-FILTER total, a row that expands in
// place onto everything known about that lead, and a dial link on the number.
//
// Filtering and paging go through router.replace inside a transition, never a native
// `<form method="GET">`: a GET submit is a full DOCUMENT navigation, which tears the shell down and
// takes the drawer with it. router.replace re-renders only this route's server tree, so the drawer
// stays open on the same panel and only the list underneath changes. This is the same staged
// pattern the vendor register's filter bar uses, for the same reasons.

import { ChevronDown, ChevronRight, Phone, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadParamNames } from "./sales-lead-params";

/** One editable field of a lead, described by the caller so this board owns no vocabulary. */
export type LeadEditField =
  | { kind: "text"; name: string; label: string; value: string; required?: boolean }
  | { kind: "date"; name: string; label: string; value: string }
  | {
      kind: "select";
      name: string;
      label: string;
      value: string;
      blankLabel: string;
      options: { value: string; label: string }[];
    };

/** One pipeline row, already reduced to the shape this board renders. */
export type LeadRow = {
  id: string;
  name: string;
  /** The one-line summary beside the name — place, animal, district; already composed and labelled. */
  detail: string;
  status: string;
  phone: string;
  /** Every editable field EXCEPT the phone number and the call status, which the board owns. */
  fields: LeadEditField[];
};

export function SalesLeadBoard({
  pageContract,
  heading,
  searchLabelKey,
  searchPlaceholderKey,
  emptySearchKey,
  emptyUnsetKey,
  saveLabelKey,
  totalLabelKey,
  rowHintKey,
  rows,
  total,
  limit,
  offset,
  search,
  status,
  statusOptions,
  params,
  returnTo,
  openLeadId,
  statusAction,
  editAction,
}: {
  pageContract: AdminUiPageContract;
  heading: string;
  searchLabelKey: string;
  searchPlaceholderKey: string;
  emptySearchKey: string;
  emptyUnsetKey: string;
  saveLabelKey: string;
  totalLabelKey: string;
  rowHintKey: string;
  rows: LeadRow[];
  /** The backend's WHOLE-FILTER count. The pager is built from this, never from rows.length. */
  total: number;
  limit: number;
  offset: number;
  search: string;
  status: string;
  statusOptions: string[];
  params: LeadParamNames;
  /** Where a save returns to — this panel, with the search, facet and page it was saved from. */
  returnTo: string;
  /** The lead a save just came back from, so the row it was made on reopens rather than collapsing. */
  openLeadId: string;
  statusAction: (formData: FormData) => void | Promise<void>;
  editAction: (formData: FormData) => void | Promise<void>;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [draft, setDraft] = useState({ search, status });
  // Expansion is CLIENT state, so opening a row costs nothing and asks the server for nothing. It
  // is seeded from the URL only after a save, which is a real navigation the row would otherwise
  // be closed by.
  const [expandedId, setExpandedId] = useState<string>(openLeadId);

  // Re-sync the draft when the SERVER answers with different applied values (Back/Forward, or
  // Clear). Adjusted during render rather than in an effect so the controls never flash the stale
  // selection. Same shape as the vendor filter bar.
  const appliedKey = JSON.stringify([search, status]);
  const [syncedKey, setSyncedKey] = useState(appliedKey);
  if (syncedKey !== appliedKey) {
    setSyncedKey(appliedKey);
    setDraft({ search, status });
  }

  const staged = draft.search.trim() !== search || draft.status !== status;
  const effectiveStatus = draft.status;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const hasAnyFilter = Boolean(search) || Boolean(status);
  const uncontacted = copy(pageContract, "value.status.uncontacted");

  /**
   * Writes the board's own parameters onto the CURRENT url and re-renders this route's server tree.
   *
   * Built from window.location because the drawer was opened by a local overlay link, which changes
   * history WITHOUT telling Next's router — so `panel` lives only in the
   * address bar, and rebuilding the query from the router's view of it would close the drawer.
   */
  function go(next: { search: string; status: string }, nextOffset: number): void {
    const url = new URL(window.location.href);
    const set = (key: string, value: string) => {
      if (value) url.searchParams.set(key, value);
      else url.searchParams.delete(key);
    };
    set(params.search, next.search.trim());
    set(params.status, next.status);
    set(params.offset, nextOffset > 0 ? String(nextOffset) : "");
    // A save's outcome banner belongs to that save, not to the next page of a list — and the row it
    // reopened is not the row the reader is now looking at.
    for (const stale of ["action_status", "action_key", params.open]) url.searchParams.delete(stale);
    startTransition(() => router.replace(`${url.pathname}${url.search}`, { scroll: false }));
  }

  /** Applying a filter always RESETS to page 1: page 5 of a now-shorter result is an empty list. */
  const apply = (next: { search: string; status: string }) => go(next, 0);

  const statusSelectOptions = (current: string) => (
    <>
      <option value="">{uncontacted}</option>
      {statusOptions.map((option) => (
        <option key={option} value={option}>
          {option}
        </option>
      ))}
      {current && !statusOptions.includes(current) ? <option value={current}>{current}</option> : null}
    </>
  );

  return (
    <>
      <div className="mt">{heading}</div>

      <div className="fchipsbar sales-leadfilters">
        <Search className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
        <input
          type="search"
          className="input"
          value={draft.search}
          onChange={(event) => setDraft({ ...draft, search: event.target.value })}
          onKeyDown={(event) => {
            // Enter applies the WHOLE staged bar, not just the term — otherwise typing a name and
            // pressing Enter would silently discard the call status just picked beside it.
            if (event.key === "Enter") {
              event.preventDefault();
              apply(draft);
            }
          }}
          placeholder={copy(pageContract, searchPlaceholderKey)}
          aria-label={copy(pageContract, searchLabelKey)}
        />
        <select
          className="input"
          value={effectiveStatus}
          onChange={(event) => setDraft({ ...draft, status: event.target.value })}
          aria-label={copy(pageContract, "filter.lead_status")}
        >
          <option value="">{copy(pageContract, "filter.lead_status.all")}</option>
          {/* The not-yet-called bucket is a real answer the pipeline chart reports, so it is
              selectable here even though no row stores that word. */}
          <option value="uncontacted">{uncontacted}</option>
          {statusOptions.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
        {/* Disabled with nothing staged, so the control tells the truth about whether pressing it
            would change anything. */}
        <button
          type="button"
          className="btn p sm"
          onClick={() => apply(draft)}
          disabled={pending || !staged}
          aria-disabled={pending || !staged}
          title={staged ? undefined : copy(pageContract, "filter.apply.nothing_staged")}
        >
          {pending ? copy(pageContract, "filter.applying") : copy(pageContract, "filter.apply")}
        </button>
        {hasAnyFilter || staged ? (
          <button type="button" className="chip" onClick={() => apply({ search: "", status: "" })} disabled={pending}>
            {copy(pageContract, "filter.clear")}
          </button>
        ) : null}
      </div>

      <div className="sales-leadcount muted small">
        <b>{total}</b> {copy(pageContract, totalLabelKey)} · {copy(pageContract, rowHintKey)}
      </div>

      {rows.length === 0 ? (
        <div className="empty">{copy(pageContract, hasAnyFilter ? emptySearchKey : emptyUnsetKey)}</div>
      ) : (
        <div className="sales-leadlist">
          {rows.map((lead) => {
            const expanded = expandedId === lead.id;
            return (
              <div key={lead.id} className={`sales-leaditem${expanded ? " on" : ""}`}>
                {/* The fast path, kept exactly as it was: pick a status and press Update while
                    working down a call list, without opening anything. */}
                <form action={statusAction} className="sales-leadrow">
                  <input type="hidden" name="lead_id" value={lead.id} />
                  <input type="hidden" name="return_to" value={returnTo} />
                  <button
                    type="button"
                    className="sales-leadname"
                    aria-expanded={expanded}
                    title={expanded ? copy(pageContract, "action.collapse_lead") : copy(pageContract, "action.expand_lead")}
                    onClick={() => setExpandedId(expanded ? "" : lead.id)}
                  >
                    {expanded ? (
                      <ChevronDown className="ic" aria-hidden="true" />
                    ) : (
                      <ChevronRight className="ic" aria-hidden="true" />
                    )}
                    <span className="sales-leadnametext">
                      <b>{lead.name}</b>
                      {lead.detail ? <span className="muted small"> · {lead.detail}</span> : null}
                    </span>
                  </button>
                  <select name="call_status" defaultValue={lead.status} aria-label={copy(pageContract, "filter.lead_status")}>
                    {statusSelectOptions(lead.status)}
                  </select>
                  <button type="submit" className="btn sm">
                    {copy(pageContract, "action.update_status")}
                  </button>
                </form>

                {expanded ? (
                  <div className="sales-leaddetail">
                    {/* The number is the point of a call list, so it leads the panel and dials on a
                        tap. A lead with none says so, and says where to put it. */}
                    <div className="sales-leadphone">
                      <Phone className="ic" aria-hidden="true" />
                      {lead.phone ? (
                        <a href={`tel:${lead.phone.replace(/\s+/g, "")}`} title={copy(pageContract, "action.call")}>
                          {lead.phone}
                        </a>
                      ) : (
                        <span className="muted">
                          {copy(pageContract, "value.no_phone")} · {copy(pageContract, "hint.no_phone")}
                        </span>
                      )}
                    </div>

                    <form action={editAction}>
                      <input type="hidden" name="lead_id" value={lead.id} />
                      {/* The row reopens where the save was made, so a corrected number can be read
                          back without hunting for the lead again. */}
                      <input
                        type="hidden"
                        name="return_to"
                        value={appendParam(returnTo, params.open, lead.id)}
                      />
                      <div className="sales-leadfields">
                        {lead.fields.map((field) => (
                          <div className="fld" key={field.name}>
                            <label htmlFor={`ed-${lead.id}-${field.name}`}>{field.label}</label>
                            {field.kind === "select" ? (
                              <select id={`ed-${lead.id}-${field.name}`} name={field.name} defaultValue={field.value}>
                                <option value="">{field.blankLabel}</option>
                                {field.options.map((option) => (
                                  <option key={option.value} value={option.value}>
                                    {option.label}
                                  </option>
                                ))}
                              </select>
                            ) : (
                              <input
                                id={`ed-${lead.id}-${field.name}`}
                                name={field.name}
                                type={field.kind === "date" ? "date" : "text"}
                                defaultValue={field.value}
                                required={field.kind === "text" && field.required}
                                maxLength={field.kind === "date" ? undefined : 160}
                              />
                            )}
                          </div>
                        ))}
                        <div className="fld">
                          <label htmlFor={`ed-${lead.id}-phone_number`}>{copy(pageContract, "field.phone_number")}</label>
                          <input
                            id={`ed-${lead.id}-phone_number`}
                            name="phone_number"
                            type="tel"
                            defaultValue={lead.phone}
                            maxLength={160}
                          />
                        </div>
                        <div className="fld">
                          <label htmlFor={`ed-${lead.id}-call_status`}>{copy(pageContract, "field.call_status")}</label>
                          <select id={`ed-${lead.id}-call_status`} name="call_status" defaultValue={lead.status}>
                            {statusSelectOptions(lead.status)}
                          </select>
                        </div>
                      </div>
                      <div className="df" style={{ padding: 0, border: 0, marginTop: 8 }}>
                        <button type="submit" className="btn p sm">
                          {copy(pageContract, saveLabelKey)}
                        </button>
                      </div>
                    </form>
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}

      {pageCount > 1 ? (
        <div className="pager2 sales-leadpager">
          <span className="muted">
            {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount}
          </span>
          <button
            type="button"
            className="btn sm"
            disabled={pending || pageNumber <= 1}
            aria-disabled={pending || pageNumber <= 1}
            onClick={() => go({ search, status }, Math.max(0, offset - limit))}
          >
            {copy(pageContract, "action.prev_page")}
          </button>
          <button
            type="button"
            className="btn sm"
            disabled={pending || pageNumber >= pageCount}
            aria-disabled={pending || pageNumber >= pageCount}
            onClick={() => go({ search, status }, offset + limit)}
          >
            {copy(pageContract, "action.next_page")}
          </button>
        </div>
      ) : null}
    </>
  );
}

/** Adds one parameter to a same-origin path built by the server. */
function appendParam(href: string, key: string, value: string): string {
  const [path, query = ""] = href.split("?", 2);
  const search = new URLSearchParams(query);
  search.set(key, value);
  return `${path}?${search.toString()}`;
}
