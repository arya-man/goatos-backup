"use client";

import { useMemo, useState, useTransition } from "react";
import { AlertTriangle, CalendarDays, Pencil, Plus, X } from "lucide-react";
import { publishVersion, runImpactPreview, saveDraft, type ActionResult } from "./config-actions";
import {
  buildRuleDsl,
  newDose,
  sourceBadge,
  validatePublish,
  BREEDS,
  CATCH_UPS,
  CATEGORIES,
  DEFER_STATES,
  FEED_CLASSES,
  FEED_INVENTORY_POLICIES,
  FEED_ITEMS,
  FEED_UNITS,
  HEALTHS,
  LIFECYCLES,
  MISSED_DOSE_POLICIES,
  REPEATS,
  REPRODUCTIVE,
  REVIEW_STATUSES,
  SCOPES,
  SEXES,
  SOP_VERSIONS,
  SOURCE_SYSTEMS,
  STAGES,
  TRIGGER_TYPES,
  type DoseRow,
  type FeedFields,
  type RuleInput,
  newFeedFields,
} from "./rule-dsl";
import type { ImpactPreviewResult } from "@/lib/api/server";

const TONE_CLASS = { warn: "t-warn", info: "t-info", ok: "t-ok" } as const;

const DEFAULT_VACCINATION_ESCALATION = "miss -> Asst -> Park Head -> PHC Director; overdue -> escalate";
const DEFAULT_FEED_ESCALATION = "missed session -> Park Head -> Feed Director; stock gap -> Data Ops review";

function defaultEscalation(category: string): string {
  return category === "feed_direction" ? DEFAULT_FEED_ESCALATION : DEFAULT_VACCINATION_ESCALATION;
}

function protocolCodePlaceholder(category: string): string {
  if (category === "feed_direction") return "feed_direction.daily_ration";
  if (category === "deworming") return "deworming.famacha";
  return `${category}.enterotox`;
}

function protocolNamePlaceholder(category: string): string {
  if (category === "feed_direction") return "Daily ration - stage plan";
  if (category === "deworming") return "FAMACHA-triggered deworming";
  return "Enterotoxaemia";
}

export function RuleEditorModal({
  open,
  onClose,
  initialCategory,
  canPublish = true,
}: {
  open: boolean;
  onClose: () => void;
  initialCategory: string;
  canPublish?: boolean;
}) {
  const [category, setCategory] = useState(initialCategory);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [scope, setScope] = useState("tenant");
  const [effectiveFrom, setEffectiveFrom] = useState("");

  const [stage, setStage] = useState("K1");
  const [sex, setSex] = useState("all");
  const [breed, setBreed] = useState("all");
  const [health, setHealth] = useState("healthy");
  const [lifecycle, setLifecycle] = useState("active");
  const [reproductive, setReproductive] = useState("any");
  const [deferStates, setDeferStates] = useState<string[]>([...DEFER_STATES]);
  const [individualOverride, setIndividualOverride] = useState(true);
  const [vaccineLotPolicy, setVaccineLotPolicy] = useState("FEFO lot required; cold-chain and expiry checked before verification");

  const [missedDosePolicy, setMissedDosePolicy] = useState("phc_approval");
  const [escalation, setEscalation] = useState(() => defaultEscalation(initialCategory));

  const [sourceSystem, setSourceSystem] = useState("manual_admin");
  const [sourceRef, setSourceRef] = useState("");
  const [reviewStatus, setReviewStatus] = useState("extracted");
  const [reviewedBy, setReviewedBy] = useState("");
  const [approvedBy, setApprovedBy] = useState("");

  const [doses, setDoses] = useState<DoseRow[]>([newDose(1)]);
  const [feed, setFeed] = useState<FeedFields>(() => newFeedFields());

  const [versionId, setVersionId] = useState("");
  const [notice, setNotice] = useState<ActionResult | null>(null);
  const [impact, setImpact] = useState<ImpactPreviewResult | null>(null);
  const [pending, startTransition] = useTransition();
  const isFeedDirection = category === "feed_direction";

  const input: RuleInput = useMemo(
    () => ({
      category,
      code,
      name,
      scope,
      effectiveFrom,
      eligibility: { stage, sex, breed, lifecycle, health, reproductive, deferStates, individualOverride },
      vaccineLotPolicy,
      missedDosePolicy,
      escalation,
      source: { sourceSystem, sourceRef, reviewStatus, reviewedBy, approvedBy },
      doses,
      feed,
    }),
    [
      category, code, name, scope, effectiveFrom, stage, sex, breed, lifecycle, health, reproductive,
      deferStates, individualOverride, vaccineLotPolicy, missedDosePolicy, escalation, sourceSystem, sourceRef,
      reviewStatus, reviewedBy, approvedBy, doses, feed,
    ],
  );

  const dsl = useMemo(() => buildRuleDsl(input), [input]);
  const badge = sourceBadge(input.source);
  const publishGate = validatePublish(input.source);

  function setDose(i: number, patch: Partial<DoseRow>) {
    setDoses((rows) => rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  }
  function toggleDefer(value: string) {
    setDeferStates((prev) => (prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value]));
  }
  function changeCategory(nextCategory: string) {
    setCategory(nextCategory);
    setEscalation(defaultEscalation(nextCategory));
  }
  function setFeedField(patch: Partial<FeedFields>) {
    setFeed((prev) => ({ ...prev, ...patch }));
  }

  function preview() {
    if (category !== "vaccination") return;
    startTransition(async () => {
      const res = await runImpactPreview({
        stage,
        sex,
        breed,
        doses_per_goat: doses.length,
        dose_rows: doses.length,
        horizon_days: 30,
      });
      if (res.ok && res.data) setImpact(res.data);
      else setNotice({ ok: false, message: res.message ?? "preview failed" });
    });
  }

  function save() {
    startTransition(async () => {
      const res = await saveDraft(input);
      setNotice(res);
      if (res.ok && res.versionId) setVersionId(res.versionId);
    });
  }

  function publish() {
    startTransition(async () => setNotice(await publishVersion(versionId, input.source)));
  }

  if (!open) return null;

  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div className="cfgmodal on" style={{ width: "min(1180px,96vw)" }} role="dialog" aria-modal="true" aria-label="Protocol rule editor">
        <div className="cmh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <Pencil className="ic" />
          </span>
          <div>
            <div className="b700">New draft rule</div>
            <div className="muted small">
              stored as rule_dsl (JSONB · JSON-Schema-validated · not YAML) · category changes fields and payload
            </div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label="Close">
            <X className="ic" />
          </button>
        </div>

        <div className="cmb">
          <div className="cfgform">
            {notice ? (
              notice.ok ? (
                <div className="note">
                  <span className="tag t-ok">ok</span> {notice.message}
                </div>
              ) : (
                <div className="alert warn">
                  <AlertTriangle className="ic" />
                  <div>{notice.message}</div>
                </div>
              )
            ) : null}

            <label>Module / category</label>
            <select aria-label="Module / category" value={category} onChange={(e) => changeCategory(e.target.value)}>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>

            <label>Protocol code · name</label>
            <div className="rowf">
              <input aria-label="Protocol code" value={code} onChange={(e) => setCode(e.target.value)} placeholder={protocolCodePlaceholder(category)} />
              <input aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder={protocolNamePlaceholder(category)} />
            </div>

            <label>Scope · effective from</label>
            <div className="rowf">
              <select aria-label="Scope" value={scope} onChange={(e) => setScope(e.target.value)}>
                {SCOPES.map((s) => (
                  <option key={s} value={s}>
                    {s === "tenant" ? "tenant (company default)" : s.replace(":", ": ")}
                  </option>
                ))}
              </select>
              <input type="date" aria-label="Effective from" value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} />
            </div>

            {isFeedDirection ? (
              <>
                <label>Feed Direction - animal stage / class</label>
                <div className="rowf">
                  <select aria-label="Feed animal stage" value={feed.animalStage} onChange={(e) => setFeedField({ animalStage: e.target.value })}>
                    {STAGES.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                  <select aria-label="Breed or class" value={feed.breedClass} onChange={(e) => setFeedField({ breedClass: e.target.value })}>
                    {FEED_CLASSES.map((s) => (
                      <option key={s} value={s}>
                        {s.replace(/_/g, " ")}
                      </option>
                    ))}
                  </select>
                </div>

                <label>Ration / feed item - quantity</label>
                <div className="rowf">
                  <select aria-label="Feed item" value={feed.feedItem} onChange={(e) => setFeedField({ feedItem: e.target.value })}>
                    {FEED_ITEMS.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                  <input aria-label="Quantity" type="number" min="0" step="0.01" value={feed.quantity} onChange={(e) => setFeedField({ quantity: Number(e.target.value) })} />
                  <select aria-label="Unit" value={feed.unit} onChange={(e) => setFeedField({ unit: e.target.value })}>
                    {FEED_UNITS.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                </div>

                <label>Session timing</label>
                <input
                  aria-label="Session timing"
                  value={feed.sessionTimes}
                  onChange={(e) => setFeedField({ sessionTimes: e.target.value })}
                  placeholder="09:00,15:00"
                />

                <label>Packing / execution proof</label>
                <div className="rowf">
                  <input
                    aria-label="Packing proof policy"
                    value={feed.packingProofCsv}
                    onChange={(e) => setFeedField({ packingProofCsv: e.target.value })}
                    placeholder="pack_qty,feed_item,lot,video"
                  />
                  <input
                    aria-label="Execution proof policy"
                    value={feed.executionProofCsv}
                    onChange={(e) => setFeedField({ executionProofCsv: e.target.value })}
                    placeholder="distribution_video,consumed_qty,water_check"
                  />
                </div>

                <label>Inventory reserve / consume / release</label>
                <select aria-label="Inventory policy" value={feed.inventoryPolicy} onChange={(e) => setFeedField({ inventoryPolicy: e.target.value })}>
                  {FEED_INVENTORY_POLICIES.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>

                <label>Escalation policy</label>
                <input aria-label="Escalation policy" value={escalation} onChange={(e) => setEscalation(e.target.value)} />
              </>
            ) : (
              <>
                <label>Vaccination eligibility - animal / shed stage + sex</label>
                <div className="rowf">
                  <select aria-label="Stage" value={stage} onChange={(e) => setStage(e.target.value)}>
                    {STAGES.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                  <select aria-label="Sex" value={sex} onChange={(e) => setSex(e.target.value)}>
                    {SEXES.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <select aria-label="Breed" value={breed} onChange={(e) => setBreed(e.target.value)}>
                    {BREEDS.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                  <select aria-label="Health" value={health} onChange={(e) => setHealth(e.target.value)}>
                    {HEALTHS.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                </div>

                <label>Lifecycle / reproductive</label>
                <div className="rowf">
                  <select aria-label="Lifecycle" value={lifecycle} onChange={(e) => setLifecycle(e.target.value)}>
                    {LIFECYCLES.map((s) => (
                      <option key={s}>{s}</option>
                    ))}
                  </select>
                  <select aria-label="Reproductive" value={reproductive} onChange={(e) => setReproductive(e.target.value)}>
                    {REPRODUCTIVE.map((s) => (
                      <option key={s} value={s}>
                        {s.replace(/_/g, " ")}
                      </option>
                    ))}
                  </select>
                </div>

                <label>Defer when (obligation marked deferred + event; not hidden)</label>
                <div className="cfgchk">
                  {DEFER_STATES.map((d) => (
                    <label key={d}>
                      <input type="checkbox" checked={deferStates.includes(d)} onChange={() => toggleDefer(d)} /> {d === "sick" ? "sick / under_treatment" : d}
                    </label>
                  ))}
                </div>
                <div className="cfgchk" style={{ marginTop: 8 }}>
                  <label>
                    <input type="checkbox" checked={individualOverride} onChange={(e) => setIndividualOverride(e.target.checked)} /> allow individual override / catch-up
                  </label>
                </div>

                <label>Booster / catch-up / missed-dose policy</label>
                <select aria-label="Missed-dose policy" value={missedDosePolicy} onChange={(e) => setMissedDosePolicy(e.target.value)}>
                  {MISSED_DOSE_POLICIES.map((m) => (
                    <option key={m.value} value={m.value}>
                      {m.label}
                    </option>
                  ))}
                </select>

                <label>Stock / vaccine lot requirements</label>
                <input aria-label="Vaccine lot policy" value={vaccineLotPolicy} onChange={(e) => setVaccineLotPolicy(e.target.value)} />

                <label>Escalation policy</label>
                <input aria-label="Escalation policy" value={escalation} onChange={(e) => setEscalation(e.target.value)} />
              </>
            )}

            <label>Source &amp; review (publish needs a real source + approval)</label>
            <div className="rowf">
              <select aria-label="Source system" value={sourceSystem} onChange={(e) => setSourceSystem(e.target.value)}>
                {SOURCE_SYSTEMS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
              <input aria-label="Source ref" value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} placeholder="source_ref (sheet / file id)" />
            </div>
            <div className="rowf" style={{ marginTop: 6 }}>
              <select aria-label="Review status" value={reviewStatus} onChange={(e) => setReviewStatus(e.target.value)}>
                {REVIEW_STATUSES.map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
              <input aria-label="Reviewed by" value={reviewedBy} onChange={(e) => setReviewedBy(e.target.value)} placeholder="reviewed_by" />
            </div>
            <input
              aria-label="Approved by"
              value={approvedBy}
              onChange={(e) => setApprovedBy(e.target.value)}
              placeholder="approved_by"
              style={{ marginTop: 6, width: "100%", border: "1px solid var(--line)", background: "var(--bg)", color: "var(--ink)", borderRadius: 8, padding: "8px 10px", font: "inherit", fontSize: 13 }}
            />
          </div>

          {/* Live rule_dsl JSONB preview */}
          <div>
            <label style={{ display: "block", fontSize: 11, fontWeight: 700, color: "var(--muted)", textTransform: "uppercase", letterSpacing: ".4px", marginBottom: 5 }}>
              Stored as rule_dsl (JSONB)
            </label>
            <div className="cfgjson" aria-label="rule_dsl preview">
              {JSON.stringify(dsl, null, 2)}
            </div>
            <div className="muted small" style={{ marginTop: 9, lineHeight: 1.5 }}>
              {isFeedDirection ? (
                <>
                  <b>Feed Direction uses ration + session_timing + inventory_policy</b>; no vaccination dose/lot fields
                  are shown. Source is nested under <span className="mono">source</span>. JSON-Schema-validated ·
                  versioned · not YAML.
                </>
              ) : (
                <>
                  <b>schedule = ARRAY of dose rows</b> (multi-dose / multi-phase). Canonical keys:{" "}
                  <span className="mono">repeat_until_after_age</span>, <span className="mono">proof_policy</span>.
                  Next due from <b>last accepted completion</b> (not DOB) when prior history exists. Source nested under{" "}
                  <span className="mono">source</span>. JSON-Schema-validated · versioned · not YAML.
                </>
              )}
            </div>
          </div>
        </div>

        {isFeedDirection ? (
          <>
            <div className="cmh" style={{ borderTop: "1px solid var(--line2)", borderBottom: "1px solid var(--line2)", position: "static" }}>
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>Feed Direction rule rows</h4>
              <span className={`tag ${TONE_CLASS[badge.tone]}`}>{badge.text}</span>
            </div>
            <div style={{ overflowX: "auto", padding: "0 18px 6px" }}>
              <table>
                <thead>
                  <tr>
                    <th>Session</th>
                    <th>Feed item</th>
                    <th>Quantity</th>
                    <th>Proof</th>
                    <th>Inventory policy</th>
                  </tr>
                </thead>
                <tbody>
                  {feed.sessionTimes
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                    .map((session, i) => (
                      <tr key={`${session}-${i}`}>
                        <td className="mono">{session}</td>
                        <td>{feed.feedItem}</td>
                        <td>
                          {feed.quantity} {feed.unit}
                        </td>
                        <td className="muted small">packing + execution proof</td>
                        <td className="muted small">{feed.inventoryPolicy.replace(/_/g, " ")}</td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <>
            <div className="cmh" style={{ borderTop: "1px solid var(--line2)", borderBottom: "1px solid var(--line2)", position: "static" }}>
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>Schedule builder - dose / phase rows</h4>
              <span className={`tag ${TONE_CLASS[badge.tone]}`}>{badge.text}</span>
              <div className="sp" style={{ flex: 1 }} />
              <button type="button" className="btn sm" onClick={() => setDoses((r) => [...r, newDose(r.length + 1)])}>
                <Plus className="ic" /> Add dose row
              </button>
            </div>
            <div style={{ overflowX: "auto", padding: "0 18px 6px" }}>
              <table>
                <thead>
                  <tr>
                    <th>Dose / sequence</th>
                    <th>Trigger</th>
                    <th>Offset (d)</th>
                    <th>Window (d)</th>
                    <th>Repeat</th>
                    <th>repeat_until_after_age</th>
                    <th>Min gap (d)</th>
                    <th>Catch-up</th>
                    <th>SOP</th>
                    <th>proof_policy</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {doses.map((d, i) => (
                    <tr key={i}>
                      <td style={{ minWidth: 120 }}>
                        <input aria-label="Dose code" value={d.doseCode} onChange={(e) => setDose(i, { doseCode: e.target.value })} />
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <select aria-label="Trigger" value={d.trigger} onChange={(e) => setDose(i, { trigger: e.target.value })}>
                          {TRIGGER_TYPES.map((t) => (
                            <option key={t}>{t}</option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input aria-label="Offset days" type="number" value={d.offsetDays} onChange={(e) => setDose(i, { offsetDays: Number(e.target.value) })} />
                      </td>
                      <td>
                        <input aria-label="Due window days" type="number" value={d.dueWindowDays} onChange={(e) => setDose(i, { dueWindowDays: Number(e.target.value) })} />
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select aria-label="Repeat" value={d.repeat} onChange={(e) => setDose(i, { repeat: e.target.value })}>
                          {REPEATS.map((t) => (
                            <option key={t}>{t}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <input aria-label="Repeat until after age" value={d.repeatUntilAfterAge} onChange={(e) => setDose(i, { repeatUntilAfterAge: e.target.value })} />
                      </td>
                      <td>
                        <input aria-label="Min gap days" type="number" value={d.minGapDays} onChange={(e) => setDose(i, { minGapDays: Number(e.target.value) })} />
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select aria-label="Catch up" value={d.catchUp} onChange={(e) => setDose(i, { catchUp: e.target.value })}>
                          {CATCH_UPS.map((t) => (
                            <option key={t}>{t}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <select aria-label="SOP version" value={d.sopVersion} onChange={(e) => setDose(i, { sopVersion: e.target.value })}>
                          {SOP_VERSIONS.map((t) => (
                            <option key={t}>{t}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <input aria-label="Proof policy" value={d.proofCsv} onChange={(e) => setDose(i, { proofCsv: e.target.value })} />
                      </td>
                      <td>
                        <button
                          type="button"
                          aria-label="Remove dose"
                          className="btn sm"
                          onClick={() => setDoses((r) => r.filter((_, idx) => idx !== i))}
                          disabled={doses.length === 1}
                          style={doses.length === 1 ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
                        >
                          <X className="ic" />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}

        {/* Impact preview */}
        <div className="cmh" style={{ borderTop: "1px solid var(--line2)", position: "static" }}>
          <h4 style={{ margin: 0 }}>
            Impact preview - computed from {isFeedDirection ? "ration/session rules" : "schedule[]"}
          </h4>
          <div className="sp" style={{ flex: 1 }} />
          {category === "vaccination" ? (
            <button type="button" className="btn sm" onClick={preview} disabled={pending}>
              {pending ? "Computing…" : "Preview impact"}
            </button>
          ) : (
            <span className="muted small">backend preview lands with the {category} domain</span>
          )}
        </div>
        <div style={{ padding: "0 18px 12px" }}>
          {impact ? (
            <>
              <div className="grid g4">
                {[
                  ["Eligible goats", impact.eligible_goats],
                  ["Obligations / cycle", impact.obligations],
                  ["Batches (SOP tasks)", impact.batches],
                  ["Doses required", impact.doses_required],
                ].map(([l, v]) => (
                  <div key={String(l)} className="kpi">
                    <div className="lab">{l}</div>
                    <div className="val">{String(v)}</div>
                  </div>
                ))}
              </div>
              <div className="muted small" style={{ marginTop: 10 }}>
                doses available: <b>{impact.doses_available || "—"}</b>
                {impact.earliest_expiry ? ` · earliest expiry ${impact.earliest_expiry.slice(0, 10)}` : ""}
              </div>
              {impact.warnings.length > 0
                ? impact.warnings.map((w, i) => (
                    <div key={i} className="alert warn" style={{ marginTop: 8 }}>
                      <AlertTriangle className="ic" />
                      <div>{w}</div>
                    </div>
                  ))
                : null}
            </>
          ) : (
            <p className="muted small">
              {isFeedDirection
                ? "Feed preview will compute eligible sheds/classes, session obligations, packing batches, and reserve/consume/release inventory once the feed domain endpoint lands."
                : "Run a preview to compute live eligible goats, obligations, drive batches, and doses-vs-stock from the schedule."}
            </p>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <div className="sp" style={{ flex: 1 }} />
          {versionId ? <span className="muted small">draft saved · {versionId.slice(0, 8)}</span> : null}
          <button type="button" className="btn" onClick={save} disabled={pending}>
            {pending ? "Saving…" : "Save draft"}
          </button>
          <button
            type="button"
            className="btn p"
            onClick={publish}
            disabled={pending || !versionId || !canPublish || !publishGate.ok}
            title={!canPublish ? "Only CEO/COO can publish" : !versionId ? "Save the draft first" : publishGate.ok ? "" : publishGate.message}
            style={pending || !versionId || !canPublish || !publishGate.ok ? { opacity: 0.45 } : undefined}
          >
            {canPublish ? "Publish" : "Publish (CEO/COO)"}
          </button>
        </div>
      </div>
    </>
  );
}
