"use client";

/**
 * Vaccination plan console — the list screen.
 *
 * Markup and class names are the approved mock's, verbatim; the styles live in
 * app/mesha-theme.css under `.vp`. Two screens, mirroring what the product
 * already has: this list, and the editor behind "Start a new version". They are
 * deliberately NOT merged — see MOCK-BEHAVIOUR-SPEC.md §2.
 *
 * Copy rules from that spec, enforced here:
 *   - no schema vocabulary on screen (no obligation / rule_dsl / scope / offset)
 *   - version IDs never shown; the label is the identity a person recognises
 *   - no dead text: a card with nothing to say does not render
 */

import { useState, useTransition } from "react";

import type { ProtocolConfigItem } from "@/lib/api/server";

import { startNewVersion } from "./plan-actions";
import { describeFirstDoses, describeRepeats, type VaccineGroup } from "./plan-model";

type Props = {
  versions: ProtocolConfigItem[];
  liveVaccines: VaccineGroup[];
  loadError: string | null;
};

export function VaccinationPlanConsole({ versions, liveVaccines, loadError }: Props) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  const live = versions.find((v) => v.status === "published");
  const draft = versions.find((v) => v.status === "draft");
  const earlier = versions
    .filter((v) => v.status === "retired")
    .sort((a, b) => (b.version ?? 0) - (a.version ?? 0));

  function onStart() {
    setError(null);
    startTransition(async () => {
      const result = await startNewVersion();
      if (!result.ok) setError(result.error);
    });
  }

  if (loadError) {
    return (
      <div className="vp">
        <section className="card">
          <div className="card-b">
            <p className="alert">{loadError}</p>
          </div>
        </section>
      </div>
    );
  }

  const draftHref = draft ? `/vaccination/plan/edit?version=${draft.protocol_version_id}` : "#";

  return (
    <div className="vp">
      <header className="head">
        <div className="head-top">
          <div>
            <div className="eyebrow">Preventive Care</div>
            <h1>Vaccination plan</h1>
            <p className="sub">
              One plan decides which animal gets which vaccine, and when. Only you and the COO can
              publish it.
            </p>
          </div>
          <div className="hactions">
            {draft ? (
              <a className="btn" href={draftHref}>
                Open {draft.version_label || `V${draft.version}`}
              </a>
            ) : (
              <button className="btn" onClick={onStart} disabled={pending} type="button">
                {pending ? "Starting…" : "Start a new version"}
              </button>
            )}
          </div>
        </div>
        {error ? <p className="alert">{error}</p> : null}
      </header>

      {live ? (
        <section className="card livecard">
          <div className="card-h">
            <div>
              <div className="eyebrow" style={{ marginBottom: 4 }}>
                Live right now
              </div>
              <h2>{live.version_label || `V${live.version}`}</h2>
            </div>
            <span className="pill live">
              <span className="dot" />
              Published
            </span>
          </div>
          <div className="card-b">
            <div className="livegrid">
              <div>
                <div className="k">In force since</div>
                <div className="lv num">{formatDate(live.effective_from)}</div>
              </div>
              <div>
                <div className="k">Applies to</div>
                <div className="lv">{appliesTo(live)}</div>
              </div>
              <div>
                <div className="k">Vaccines in the plan</div>
                <div className="lv">
                  <span className="num">{liveVaccines.length}</span>
                </div>
              </div>
              <div>
                <div className="k">Published</div>
                {/* The publisher's name is shown only when the record has one.
                    Older rows were written by an import and have no author, and
                    a dash beside a date reads as a broken field rather than as
                    "nobody" — so the date stands alone instead. */}
                <div className="lv">
                  {live.published_by ? (
                    <>
                      {live.published_by}
                      <span className="vby">{formatDate(live.published_at)}</span>
                    </>
                  ) : (
                    <span className="num">{formatDate(live.published_at)}</span>
                  )}
                </div>
              </div>
            </div>

            {liveVaccines.length > 0 ? (
              <div className="scroll">
                <table className="tabl">
                  <thead>
                    <tr>
                      <th>Vaccine</th>
                      <th>First doses</th>
                      <th>Repeats</th>
                    </tr>
                  </thead>
                  <tbody>
                    {liveVaccines.map((v) => (
                      <tr key={v.code}>
                        <td>
                          <b>{v.name}</b>
                        </td>
                        <td>{describeFirstDoses(v.firstDoses)}</td>
                        <td>{describeRepeats(v.repeats)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}

            {draft ? (
              <div className="draftnudge">
                <span className="dn-l">
                  <b>A draft is waiting.</b> {draft.version_label || `V${draft.version}`} — not live
                  yet.
                </span>
                <span className="ab-spacer" />
                <a className="btn sm" href={draftHref}>
                  Open the draft
                </a>
              </div>
            ) : null}
          </div>
        </section>
      ) : null}

      {earlier.length > 0 ? (
        <section className="card">
          <div className="card-h">
            <div>
              <h2>Earlier versions</h2>
              <p className="s">
                Every published plan is kept. Publishing retires the current one and starts a new
                number — nothing is overwritten.
              </p>
            </div>
          </div>
          <div className="card-b">
            <div className="scroll">
              <table className="tabl">
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>In force</th>
                    <th>Published</th>
                  </tr>
                </thead>
                <tbody>
                  {earlier.map((v) => (
                    <tr className="voff" key={v.protocol_version_id}>
                      <td>
                        <b>{v.version_label || `V${v.version}`}</b>
                      </td>
                      <td className="num">
                        {formatDate(v.effective_from)} – {formatDate(v.effective_to)}
                      </td>
                      <td className="num">
                        {formatDate(v.published_at)}
                        {v.published_by ? <span className="vby">{v.published_by}</span> : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      ) : null}
    </div>
  );
}

/**
 * "Both parks" / the park's own name.
 *
 * scope_type is the schema's word ("tenant"), and the spec forbids schema
 * vocabulary on screen. A tenant-scoped plan governs every park, which is what
 * the reader needs to know.
 */
function appliesTo(live: ProtocolConfigItem): string {
  if (live.scope_type === "park") return live.scope_label || "One park";
  return "Both parks";
}

function formatDate(value: string | undefined | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}
