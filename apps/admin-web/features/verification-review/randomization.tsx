import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getVerificationSampling } from "@/lib/api/server";
import { setVerificationSamplingPoliciesAction } from "./randomization-actions";
import { InfoHint } from "@/components/app/info-hint";

/**
 * RANDOMIZATION: per module, the share of that module's proof videos the verifier actually has to
 * watch, and today's progress against that share (maintainer decision 2026-08-26).
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "randomization", false)` before
 * rendering this component (see verification-review-page.tsx) -- the SAME capability
 * (permissions.VerificationSampling) gates both the page contract control and the backend endpoint
 * this component reads, so a verifier's -- or a director's -- build never even calls the endpoint.
 * This component reads the contract for copy only, never for its own gating decision.
 *
 * EVERY NUMBER HERE IS SERVER-COMPUTED, including progress. A share of 40% fully reviewed reads
 * 100%, and that arithmetic lives in the backend so the panel, a future phone screen and any report
 * cannot each derive a different completion number for the same day.
 *
 * Every visible word is backend copy: the module and page labels come from the verification
 * registry, and a locked row's reason is composed by the backend. The category token that
 * identifies a row is submitted in a hidden field and never printed -- it is config vocabulary, and
 * the copy firewall bans it from visible UI.
 */
export async function Randomization({
  pageContract,
  returnTo,
}: {
  pageContract: AdminUiPageContract;
  /** The live page URL, so a save returns to the queue the CEO was looking at. */
  returnTo: string;
}) {
  const result = await getVerificationSampling({});
  if (!result.ok) {
    return <div className="small muted">{copy(pageContract, "randomization.unavailable")}</div>;
  }
  const { categories, business_date: businessDate } = result.data;

  const editable = categories.some((row) => row.waivable);
  return (
    <form action={setVerificationSamplingPoliciesAction} className="vr-osec vr-rand-sheet">
      <input type="hidden" name="return_to" value={returnTo} />
      {/* Part of every save's idempotency identity -- see the action. */}
      <input type="hidden" name="business_date" value={businessDate} />
      <div className="kit-section-bar">
        <span className="kit-section-bar-title">
          {copy(pageContract, "randomization.col.share")}
          <InfoHint text={copy(pageContract, "randomization.share_help")} />
        </span>
        {editable ? (
          <button type="submit" className="btn sm p">
            {copy(pageContract, "randomization.apply")}
          </button>
        ) : null}
      </div>
      <div className="vr-omods">
        {categories.map((row) => {
          // Captured, selected and reviewed are NOT disjoint -- selected is a subset of captured,
          // reviewed a subset of selected -- so they are read out separately and never summed.
          const share = row.waivable ? `${row.sample_percent}%` : copy(pageContract, "randomization.locked");
          const done = row.progress_percent >= 100;
          return (
            <div key={row.category} className="vr-omod vr-rand">
              <div className="t">
                <span className="n">
                  {row.module_label} · {row.page_label}
                </span>
                <b>{share}</b>
              </div>

              {/* Progress against HER SHARE, not against the day's capture. The bar is the number
                  the maintainer asked for: at 40% sampling, all 40% reviewed fills it. */}
              <div className="bar">
                <i
                  style={{
                    width: `${Math.min(100, Math.max(0, row.progress_percent))}%`,
                    background: done ? "var(--ok)" : "var(--brand)",
                  }}
                />
              </div>
              <div className="m">
                {done
                  ? copy(pageContract, "randomization.complete")
                  : `${row.reviewed}/${row.selected} ${copy(pageContract, "randomization.reviewed")}`}
              </div>

              <div className="vr-rand-day">
                <span>
                  {row.captured} {copy(pageContract, "randomization.videos_arrived")}
                </span>
                <span>
                  {row.selected} {copy(pageContract, "randomization.to_review")}
                </span>
                {row.auto_accepted > 0 ? (
                  <span>
                    {row.auto_accepted} {copy(pageContract, "randomization.settled")}
                  </span>
                ) : null}
              </div>

              {row.waivable ? (
                <div className="vr-rand-set">
                  <input type="hidden" name={`current__${row.category}`} value={row.sample_percent} />
                  <label className="fld" style={{ marginBottom: 0 }}>
                    <span>{copy(pageContract, "randomization.col.share")}</span>
                    {/* defaultValue, never value: an uncontrolled field in a server-action form.
                        No client-side clamp, and blank is NOT coerced to 0 -- the sheet's action
                        refuses it and the backend owns the range. */}
                    <input
                      type="number"
                      name={`sample_percent__${row.category}`}
                      min="0"
                      max="100"
                      step="1"
                      inputMode="numeric"
                      defaultValue={row.sample_percent}
                    />
                  </label>
                </div>
              ) : (
                <div className="vr-rand-locked small muted" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                  {copy(pageContract, "randomization.locked")}
                  {row.locked_reason ? <InfoHint text={row.locked_reason} /> : null}
                </div>
              )}

              {row.effective_from ? (
                <div className="vr-rand-meta">
                  {copy(pageContract, "randomization.effective")} {row.effective_from}
                  {row.set_by_name ? ` · ${copy(pageContract, "randomization.set_by")} ${row.set_by_name}` : ""}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </form>
  );
}
