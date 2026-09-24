"use client";

// FARM VALUATION section on Sales Config (maintainer instruction 2026-09-19; the stage list became
// the farm's own on 2026-09-24). The herd is valued in the stages written here: what each is
// called, which entries of the farm's herd register it covers, and what a female and a male in it
// are carried at. One form, one save, landing in place.
//
// The stage the valuation could not price is the reason this screen exists. On the day it was
// built 58 kids stood in Warmup -- a stage the register has always carried -- valued at nothing,
// because six stages were written into a query. So the register is shown here with its live head
// counts and the ones nothing values are offered to add in one click: the screen answers "which of
// my animals is nothing pricing" before the farm has to ask it.
//
// Every sentence is backend copy.
import { Calculator, Plus, X } from "lucide-react";
import { useActionState, useMemo, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult } from "@/lib/api/server";
import type { StageRegisterEntry, ValuationAssumptions, ValuationBucket, ValuationStage } from "@/lib/api/sales-valuation-server";
import { saveValuationAction, type ValuationActionState } from "./valuation-actions";

const INITIAL: ValuationActionState = { status: "idle", code: "", message: "", ticket: 0 };

/** A stage as the screen holds it: the authored row plus the id that keeps its inputs mounted. */
type EditRow = ValuationStage & { rid: string };
const GENDERS = [
  { key: "female", copyKey: "valuation.gender.female" },
  { key: "male", copyKey: "valuation.gender.male" },
] as const;

// The same slug the backend derives a new stage's key from, so the figures typed beside a stage
// being added land on the key it is stored under. The backend keeps a key it is given, and refuses
// one that collides with a stage already there.
function stageKeyFromLabel(label: string): string {
  return label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

function normalizeMatch(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9]/g, "");
}

export function ValuationSection({
  pageContract,
  result,
  canEdit,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  result: ApiResult<ValuationAssumptions>;
  canEdit: boolean;
  disabledReason: string;
}) {
  const [state, formAction, pending] = useActionState(saveValuationAction, INITIAL);
  // The row on screen is the last one SAVED when there is one, else the one the page loaded with:
  // the hidden row_version below must be the fence the next save is judged against.
  const v = state.saved ?? (result.ok ? result.data : undefined);
  // Each row carries a client-side id so React keeps the same inputs while the farm edits it. The
  // stage KEY cannot serve: it is blank on a row being added and would otherwise be re-derived on
  // every keystroke, remounting the input the name is being typed into.
  const [stages, setStages] = useState<EditRow[] | null>(null);
  const [nextRid, setNextRid] = useState(1);
  // A save replaces the stage list with what was stored -- including the keys the backend assigned
  // to rows that were added -- so edits are never applied on top of a list the server did not take.
  const [appliedTicket, setAppliedTicket] = useState(0);
  if (state.status === "success" && state.ticket !== appliedTicket) {
    setAppliedTicket(state.ticket);
    setStages(null);
  }
  const rows: EditRow[] = stages ?? (v?.stages ?? []).map((st) => ({ ...st, rid: st.stage }));
  const register: StageRegisterEntry[] = v?.stage_register ?? [];

  const byBucket = useMemo(() => {
    const m = new Map<string, ValuationBucket>();
    for (const b of v?.buckets ?? []) m.set(b.bucket, b);
    return m;
  }, [v]);

  // A register entry belongs to ONE valuation stage, so an entry already placed is not offered
  // again -- the screen cannot compose the write the backend would refuse.
  const placed = useMemo(() => new Set(rows.flatMap((s) => s.matches.map(normalizeMatch))), [rows]);
  const unplaced = register.filter((e) => !placed.has(normalizeMatch(e.code)));
  const unplacedWithAnimals = unplaced.filter((e) => e.live_animals > 0);

  if (!result.ok || !v) {
    return (
      <section className="card" aria-label={copy(pageContract, "section.valuation.aria")}>
        <div className="hd">
          <Calculator className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.valuation.title")}</h3>
        </div>
        <div className="bd">
          <div className="alert">{(!result.ok && result.error.message) || copy(pageContract, "error.load")}</div>
        </div>
      </section>
    );
  }

  const edit = (i: number, patch: Partial<EditRow>) => setStages(rows.map((s, n) => (n === i ? { ...s, ...patch } : s)));
  const addRow = (row: Omit<EditRow, "rid">) => {
    setStages([...rows, { ...row, rid: `new_${nextRid}` }]);
    setNextRid(nextRid + 1);
  };
  const message = state.status === "success" ? copy(pageContract, "valuation.saved") : state.status === "error" ? state.message || copy(pageContract, "valuation.error") : "";

  return (
    <section className="card" aria-label={copy(pageContract, "section.valuation.aria")} data-testid="valuation-section">
      <div className="hd">
        <Calculator className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.valuation.title")}</h3>
      </div>
      <p className="muted small sales-config-card-copy">{copy(pageContract, "section.valuation.sub")}</p>
      <div className="bd market-config-body">
        {!canEdit ? <div className="note">{disabledReason}</div> : null}
        <form action={formAction} aria-busy={pending} className="valuation-form">
          <input type="hidden" name="row_version" value={v.row_version} />
          <input
            type="hidden"
            name="stages"
            value={JSON.stringify(rows.map((s, i) => ({ stage: s.stage || s.rid, label: s.label, display_order: i + 1, matches: s.matches })))}
          />
          <div className="tablewrap sales-valuation-tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>{copy(pageContract, "valuation.stage")}</th>
                  <th>{copy(pageContract, "valuation.covers")}</th>
                  {GENDERS.map((g) => (
                    <th key={g.key} colSpan={2}>
                      {copy(pageContract, g.copyKey)}
                    </th>
                  ))}
                  <th aria-label={copy(pageContract, "valuation.stage.remove")} />
                </tr>
                <tr>
                  <th />
                  <th />
                  {GENDERS.map((g) => [
                    <th key={`${g.key}-w`} className="small muted">
                      {copy(pageContract, "valuation.fixed_weight")}
                    </th>,
                    <th key={`${g.key}-p`} className="small muted">
                      {copy(pageContract, "valuation.price_per_kg")}
                    </th>,
                  ])}
                  <th />
                </tr>
              </thead>
              <tbody>
                {rows.map((s, i) => {
                  // A row being added is named by its id until the farm leaves the name field; the
                  // key settles ONCE, on blur, so the figures typed beside it land on it.
                  const key = s.stage || s.rid;
                  return (
                    <tr key={s.rid} data-stage={key}>
                      <td>
                        <input
                          value={s.label}
                          onChange={(e) => edit(i, { label: e.target.value })}
                          onBlur={(e) => {
                            if (!s.stage && e.target.value.trim()) edit(i, { stage: stageKeyFromLabel(e.target.value) });
                          }}
                          maxLength={60}
                          required
                          disabled={!canEdit}
                          aria-label={copy(pageContract, "valuation.stage")}
                          data-testid={`valuation-stage-label-${key}`}
                        />
                      </td>
                      <td>
                        <div className="valuation-covers">
                          {s.matches.map((m) => (
                            <span key={m} className="chip sm valuation-cover-chip">
                              {m}
                              {canEdit ? (
                                <button
                                  type="button"
                                  className="btn xs ghost"
                                  aria-label={`${copy(pageContract, "valuation.covers.remove")} ${m}`}
                                  onClick={() => edit(i, { matches: s.matches.filter((x) => x !== m) })}
                                >
                                  <X className="ic xs" aria-hidden="true" />
                                </button>
                              ) : null}
                            </span>
                          ))}
                          {canEdit ? (
                            <select
                              value=""
                              aria-label={copy(pageContract, "valuation.covers.add")}
                              data-testid={`valuation-covers-add-${key}`}
                              onChange={(e) => {
                                if (e.target.value) edit(i, { matches: [...s.matches, e.target.value] });
                              }}
                            >
                              <option value="">{copy(pageContract, "valuation.covers.add")}</option>
                              {unplaced.map((e) => (
                                <option key={e.code} value={e.code}>
                                  {e.label === e.code ? e.code : `${e.label} (${e.code})`}
                                  {e.live_animals > 0 ? ` · ${e.live_animals}` : ""}
                                </option>
                              ))}
                            </select>
                          ) : null}
                        </div>
                      </td>
                      {GENDERS.map((g) => {
                        const bucket = `${key}_${g.key}`;
                        const b = byBucket.get(bucket);
                        return [
                          <td key={`${bucket}-w`}>
                            <input
                              name={`weight_${bucket}`}
                              type="number"
                              step="0.1"
                              min={v.limits.fixed_weight_kg_min}
                              max={v.limits.fixed_weight_kg_max}
                              defaultValue={b?.fixed_weight_kg ?? ""}
                              placeholder={copy(pageContract, "valuation.fixed_weight.measured")}
                              disabled={!canEdit}
                              aria-label={`${copy(pageContract, "valuation.fixed_weight")} · ${s.label} · ${copy(pageContract, g.copyKey)}`}
                            />
                          </td>,
                          <td key={`${bucket}-p`}>
                            <input
                              name={`price_${bucket}`}
                              type="number"
                              step="1"
                              min={v.limits.price_per_kg_min}
                              max={v.limits.price_per_kg_max}
                              defaultValue={b?.price_per_kg ?? ""}
                              required
                              disabled={!canEdit}
                              aria-label={`${copy(pageContract, "valuation.price_per_kg")} · ${s.label} · ${copy(pageContract, g.copyKey)}`}
                              data-testid={`valuation-price-${bucket}`}
                            />
                          </td>,
                        ];
                      })}
                      <td>
                        {canEdit ? (
                          <button
                            type="button"
                            className="btn xs ghost"
                            aria-label={`${copy(pageContract, "valuation.stage.remove")} ${s.label}`}
                            data-testid={`valuation-stage-remove-${key}`}
                            onClick={() => setStages(rows.filter((_, n) => n !== i))}
                          >
                            <X className="ic sm" aria-hidden="true" />
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <p className="muted small market-config-hint">{copy(pageContract, "valuation.fixed_weight.hint")}</p>

          {/* The stages nothing values, with the animals standing in them. This is the screen's own
              answer to the question that made the stage list data in the first place. */}
          {canEdit && unplacedWithAnimals.length > 0 ? (
            <div className="note valuation-unvalued" data-testid="valuation-unvalued">
              <span>{copy(pageContract, "valuation.unvalued")}</span>
              <div className="valuation-covers">
                {unplacedWithAnimals.map((e) => (
                  <button
                    key={e.code}
                    type="button"
                    className="btn xs"
                    data-testid={`valuation-add-register-${e.code}`}
                    onClick={() => addRow({ stage: stageKeyFromLabel(e.label === e.code ? e.code : e.label), label: e.label === e.code ? e.code : e.label, display_order: rows.length + 1, matches: [e.code] })}
                  >
                    <Plus className="ic xs" aria-hidden="true" /> {e.label === e.code ? e.code : `${e.label} (${e.code})`} · {e.live_animals}
                  </button>
                ))}
              </div>
            </div>
          ) : null}

          <div className="grid g2 market-config-columns">
            <label className="market-config-column">
              <span className="market-config-h4">{copy(pageContract, "valuation.unsold_price")}</span>
              <input name="unsold_stock_price_rupees" type="number" step="1" min={v.limits.unsold_stock_price_min} max={v.limits.unsold_stock_price_max} defaultValue={v.unsold_stock_price_rupees ?? ""} disabled={!canEdit} />
              <span className="muted small market-config-hint">{copy(pageContract, "valuation.unsold_price.hint")}</span>
            </label>
          </div>
          <div className="market-config-line" style={{ marginTop: 10, alignItems: "center", gap: 12 }}>
            {canEdit ? (
              <>
                <button
                  type="button"
                  className="btn sm"
                  data-testid="valuation-stage-add"
                  onClick={() => addRow({ stage: "", label: "", display_order: rows.length + 1, matches: [] })}
                >
                  <Plus className="ic sm" aria-hidden="true" /> {copy(pageContract, "valuation.stage.add")}
                </button>
                <button type="submit" className="btn sm primary" disabled={pending} data-testid="valuation-save">
                  {copy(pageContract, "action.valuation.label")}
                </button>
              </>
            ) : null}
            {v.updated_at ? (
              <span className="muted small">
                {copy(pageContract, "valuation.updated")} {v.updated_at}
                {v.updated_by_name ? ` · ${v.updated_by_name}` : ""}
              </span>
            ) : null}
            {message ? (
              <span role="status" className={state.status === "success" ? "market-config-msg ok" : "market-config-msg bad"} style={{ fontSize: 12, color: state.status === "success" ? "var(--ok)" : "var(--danger)" }}>
                {message}
              </span>
            ) : null}
          </div>
        </form>
      </div>
    </section>
  );
}
