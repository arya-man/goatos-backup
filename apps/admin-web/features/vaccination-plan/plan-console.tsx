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

import { useRouter } from "next/navigation";
import { useCallback, useState, useTransition } from "react";

import type { ProtocolConfigItem } from "@/lib/api/server";

import { discardDraft, readVersionSettings, startNewVersion } from "./plan-actions";
import { describeFirstDoses, describeRepeats, readVaccines, type VaccineGroup } from "./plan-model";
import { VersionSheet, type VersionSheetData } from "./version-sheet";

type Props = {
  versions: ProtocolConfigItem[];
  catalog: VaccineGroup[];
  changeNotes: Record<string, string>;
  loadError: string | null;
};

export function VaccinationPlanConsole({ versions, catalog, changeNotes, loadError }: Props) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [sheet, setSheet] = useState<VersionSheetData | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [sheetLoading, setSheetLoading] = useState(false);
  const [sheetError, setSheetError] = useState<string | null>(null);
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);

  const live = versions.find((v) => v.status === "published");
  const inPlanCount = catalog.filter((v) => v.inPlan).length;
  const draft = versions.find((v) => v.status === "draft");
  const earlier = versions
    .filter((v) => v.status === "retired")
    .sort((a, b) => (b.version ?? 0) - (a.version ?? 0));

  function onDiscard(draftVersionId: string) {
    setError(null);
    setConfirmingDiscard(false);
    startTransition(async () => {
      const result = await discardDraft(draftVersionId);
      if (!result.ok) setError(result.error);
      router.refresh();
    });
  }

  function onStart() {
    setError(null);
    startTransition(async () => {
      const result = await startNewVersion();
      if (!result.ok) {
        setError(result.error);
        router.refresh();
        return;
      }
      // There is one draft at a time, so this action has exactly one outcome:
      // you are editing it. Whether the draft was just created or already
      // existed, the button lands in the editor rather than returning to a list
      // that then asks you to press a second button to get there.
      if (result.versionId) {
        router.push(`/vaccination/plan/edit?version=${result.versionId}`);
      }
      router.refresh();
    });
  }

  const openVersion = useCallback(async (version: ProtocolConfigItem) => {
    setSheetOpen(true);
    setSheetLoading(true);
    setSheetError(null);
    setSheet(null);
    const result = await readVersionSettings(version.protocol_version_id);
    setSheetLoading(false);
    if (!result.ok) {
      setSheetError(result.error);
      return;
    }
    setSheet({
      label: version.version_label || `V${version.version}`,
      inForce: `${formatDate(version.effective_from)} – ${formatDate(version.effective_to)}`,
      published: formatDate(version.published_at),
      vaccines: readVaccines(result.ruleDsl),
    });
  }, []);

  if (loadError) {
    return (
      <div className="vplan">
        <section className="card">
          <div className="card-b">
            <div className="alert">
              <span className="ic">!</span>
              <span>{loadError}</span>
            </div>
          </div>
        </section>
      </div>
    );
  }

  const draftHref = draft ? `/vaccination/plan/edit?version=${draft.protocol_version_id}` : "#";

  return (
    <div className="vplan">
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
        {error ? (
          <div className="alert" style={{ marginTop: 16, marginBottom: 0 }}>
            <span className="ic">!</span>
            <span>{error}</span>
          </div>
        ) : null}
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
                  <span className="num">{inPlanCount}</span> of{" "}
                  <span className="num">{catalog.length}</span>
                </div>
              </div>
              <div>
                <div className="k">Published by</div>
                {/* The publisher's name is shown only when the record has one.
                    Older rows were written by an import and have no author, and
                    a dash beside a date reads as a broken field rather than as
                    "nobody" — so the date stands alone instead. */}
                <div className="lv">
                  {personName(live.published_by) ? (
                    <>
                      {personName(live.published_by)}
                      <span className="vby">{formatDate(live.published_at)}</span>
                    </>
                  ) : (
                    <span className="num">{formatDate(live.published_at)}</span>
                  )}
                </div>
              </div>
            </div>

            {catalog.length > 0 ? (
              <div className="scroll">
                <table className="tabl">
                  <thead>
                    <tr>
                      <th>Vaccine</th>
                      <th>First doses</th>
                      <th>Repeats</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {catalog.map((v) => (
                      <tr className={v.inPlan ? undefined : "voff"} key={v.code}>
                        <td>
                          <b>{v.name}</b>
                        </td>
                        <td>{v.inPlan ? describeFirstDoses(v.firstDoses) : "—"}</td>
                        <td>{v.inPlan ? describeRepeats(v.repeats) : "—"}</td>
                        <td>
                          {v.inPlan ? (
                            <span className="tag on">in the plan</span>
                          ) : (
                            <span className="tag">not in this plan</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}

            {draft ? (
              <div className="draftnudge">
                {/* Discarding destroys work that cannot be recovered, so it asks
                    first. The question is asked inline rather than through
                    window.confirm: a native dialog is outside the design system,
                    cannot be styled, and is dismissed by automation, so the
                    destructive path would never be exercised by a test. */}
                {confirmingDiscard ? (
                  <>
                    <span className="dn-l">
                      <b>Discard {draft.version_label || `V${draft.version}`}?</b> The draft and
                      everything in it is deleted. This cannot be undone.
                    </span>
                    <span className="ab-spacer" />
                    <button
                      className="btn ghost sm"
                      type="button"
                      onClick={() => setConfirmingDiscard(false)}
                    >
                      Keep it
                    </button>
                    <button
                      className="btn sm"
                      type="button"
                      disabled={pending}
                      onClick={() => onDiscard(draft.protocol_version_id)}
                    >
                      {pending ? "Discarding…" : "Yes, discard it"}
                    </button>
                  </>
                ) : (
                  <>
                    <span className="dn-l">
                      <b>A draft is waiting.</b> {draft.version_label || `V${draft.version}`} — not
                      live yet.
                    </span>
                    <span className="ab-spacer" />
                    <button
                      className="btn ghost sm"
                      type="button"
                      disabled={pending}
                      onClick={() => setConfirmingDiscard(true)}
                    >
                      Discard it
                    </button>
                    <a className="btn sm" href={draftHref}>
                      Open the draft
                    </a>
                  </>
                )}
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
                    <th>What changed</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {earlier.map((v) => (
                    <tr key={v.protocol_version_id}>
                      <td>
                        <b>{v.version_label || `V${v.version}`}</b>
                      </td>
                      <td className="num">
                        {formatDate(v.effective_from)} – {formatDate(v.effective_to)}
                      </td>
                      <td className="num">
                        {formatDate(v.published_at)}
                        {personName(v.published_by) ? (
                          <span className="vby">{personName(v.published_by)}</span>
                        ) : null}
                      </td>
                      <td>{changeNotes[v.protocol_version_id] ?? "—"}</td>
                      <td>
                        <button className="vbtn" type="button" onClick={() => void openVersion(v)}>
                          View settings
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      ) : null}

      {sheetOpen ? (
        <VersionSheet
          data={sheet}
          loading={sheetLoading}
          error={sheetError}
          onClose={() => setSheetOpen(false)}
        />
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

/**
 * A publisher's name, or nothing.
 *
 * published_by holds a user id. There is no people lookup on this screen, and a
 * raw UUID on a CEO's screen is worse than no name at all -- the spec forbids
 * showing ids, and "90000000-0000-4000-..." tells the reader strictly less than
 * the date already does.
 */
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function personName(value: string | undefined | null): string | null {
  const trimmed = (value ?? "").trim();
  if (!trimmed || UUID_RE.test(trimmed)) return null;
  return trimmed;
}

function formatDate(value: string | undefined | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}
