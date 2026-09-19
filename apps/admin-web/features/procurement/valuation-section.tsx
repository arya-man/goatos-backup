"use client";

// FARM VALUATION section on Sales Config (maintainer instruction 2026-09-19): the figures the
// live herd is valued at on Farm value (per bucket: the weight used and the rupees per kg), the
// sale-ready line, and what unsold animals are carried at on Load wise. One form, one save,
// landing in place. Every sentence is backend copy; `{kg}` is filled from the saved line.
import { Calculator } from "lucide-react";
import { useActionState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult } from "@/lib/api/server";
import type { ValuationAssumptions } from "@/lib/api/sales-valuation-server";
import { saveValuationAction, type ValuationActionState } from "./valuation-actions";

const INITIAL: ValuationActionState = { status: "idle", code: "", message: "", ticket: 0 };

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
  const fill = (s: string, kg: number) => s.replace("{kg}", String(kg));
  if (!result.ok) {
    return (
      <section className="card" aria-label={copy(pageContract, "section.valuation.aria")}>
        <div className="hd">
          <Calculator className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.valuation.title")}</h3>
        </div>
        <div className="bd">
          <div className="alert">{result.error.message || copy(pageContract, "error.load")}</div>
        </div>
      </section>
    );
  }
  // The row on screen is the last one SAVED when there is one, else the one the page loaded with:
  // the hidden row_version below must be the fence the next save is judged against.
  const v = state.saved ?? result.data;
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
          <input type="hidden" name="bucket_keys" value={v.buckets.map((b) => b.bucket).join(",")} />
          <div className="tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>{copy(pageContract, "valuation.bucket")}</th>
                  <th>{copy(pageContract, "valuation.fixed_weight")}</th>
                  <th>{copy(pageContract, "valuation.price_per_kg")}</th>
                </tr>
              </thead>
              <tbody>
                {v.buckets.map((b) => (
                  <tr key={b.bucket} data-bucket={b.bucket}>
                    <td>
                      <input name={`label_${b.bucket}`} defaultValue={b.label} maxLength={60} required disabled={!canEdit} aria-label={copy(pageContract, "valuation.bucket")} />
                    </td>
                    <td>
                      <input
                        name={`weight_${b.bucket}`}
                        type="number"
                        step="0.1"
                        min={v.limits.fixed_weight_kg_min}
                        max={v.limits.fixed_weight_kg_max}
                        defaultValue={b.fixed_weight_kg ?? ""}
                        placeholder={copy(pageContract, "valuation.fixed_weight.measured")}
                        disabled={!canEdit}
                        aria-label={`${copy(pageContract, "valuation.fixed_weight")} · ${b.label}`}
                      />
                    </td>
                    <td>
                      <input
                        name={`price_${b.bucket}`}
                        type="number"
                        step="1"
                        min={v.limits.price_per_kg_min}
                        max={v.limits.price_per_kg_max}
                        defaultValue={b.price_per_kg}
                        required
                        disabled={!canEdit}
                        aria-label={`${copy(pageContract, "valuation.price_per_kg")} · ${b.label}`}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="muted small market-config-hint">{copy(pageContract, "valuation.fixed_weight.hint")}</p>
          <div className="grid g2 market-config-columns">
            <label className="market-config-column">
              <span className="market-config-h4">{copy(pageContract, "valuation.unsold_price")}</span>
              <input name="unsold_stock_price_rupees" type="number" step="1" min={v.limits.unsold_stock_price_min} max={v.limits.unsold_stock_price_max} defaultValue={v.unsold_stock_price_rupees ?? ""} disabled={!canEdit} />
              <span className="muted small market-config-hint">{copy(pageContract, "valuation.unsold_price.hint")}</span>
            </label>
          </div>
          <div className="market-config-line" style={{ marginTop: 10, alignItems: "center", gap: 12 }}>
            {canEdit ? (
              <button type="submit" className="btn sm primary" disabled={pending} data-testid="valuation-save">
                {copy(pageContract, "action.valuation.label")}
              </button>
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
