"use client";

import { useState, useTransition } from "react";
import { AlertTriangle, CalendarDays, CheckCircle2, ShieldCheck, Syringe, TrendingUp } from "lucide-react";
import {
  publishVersion,
  runImpactPreview,
  saveDraft,
  type ActionResult,
  type DoseRow,
} from "../config-actions";
import type { ImpactPreviewResult } from "@/lib/api/server";

const triggerTypes = ["birth_age", "post_arrival", "calendar", "after_previous_completion", "manual_campaign"];
const repeats = ["none", "every_n_days", "yearly", "until_age", "after_age"];
const catchUps = ["immediate", "next_cycle", "phc_approval", "defer"];
const sourceSystems = ["", "vaccinations_db", "phc", "vet", "manual_admin", "extracted"];

function newDose(seq: number): DoseRow {
  return { doseCode: "", sequence: seq, triggerType: "birth_age", offsetDays: 0, dueWindowDays: 7, minGapDays: 0, repeat: "none", catchUp: "phc_approval" };
}

export function ScheduleBuilder() {
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [scopeType, setScopeType] = useState("tenant");
  const [scopeId, setScopeId] = useState("");
  const [effectiveFrom, setEffectiveFrom] = useState("");
  const [stage, setStage] = useState("");
  const [sex, setSex] = useState("");
  const [breed, setBreed] = useState("");
  const [parkId, setParkId] = useState("");
  const [sourceSystem, setSourceSystem] = useState("");
  const [sourceRef, setSourceRef] = useState("");
  const [reviewStatus, setReviewStatus] = useState("draft");
  const [approvedBy, setApprovedBy] = useState("");
  const [vaccineItemId, setVaccineItemId] = useState("");
  const [dosesPerGoat, setDosesPerGoat] = useState(1);
  const [doses, setDoses] = useState<DoseRow[]>([newDose(1)]);

  const [impact, setImpact] = useState<ImpactPreviewResult | null>(null);
  const [versionId, setVersionId] = useState("");
  const [notice, setNotice] = useState<ActionResult | null>(null);
  const [pending, startTransition] = useTransition();

  const sourceBacked =
    ["vaccinations_db", "phc", "vet"].includes(sourceSystem) && sourceRef.trim() !== "" && reviewStatus === "approved" && approvedBy.trim() !== "";

  function setDose(i: number, patch: Partial<DoseRow>) {
    setDoses((rows) => rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  }

  function preview() {
    startTransition(async () => {
      const res = await runImpactPreview({
        stage,
        sex,
        breed,
        park_id: parkId || undefined,
        vaccine_item_id: vaccineItemId || undefined,
        doses_per_goat: dosesPerGoat,
        dose_rows: doses.length,
        horizon_days: 30,
      });
      if (res.ok && res.data) setImpact(res.data);
      else setNotice({ ok: false, message: res.message ?? "preview failed" });
    });
  }

  function save() {
    startTransition(async () => {
      const res = await saveDraft({
        code,
        name,
        scopeType,
        scopeId,
        effectiveFrom: effectiveFrom || new Date().toISOString().slice(0, 10),
        eligibility: { stage, sex, breed, parkId },
        source: { sourceSystem, sourceRef, reviewStatus, approvedBy },
        doses,
      });
      setNotice(res);
      if (res.ok && res.versionId) setVersionId(res.versionId);
    });
  }

  function publish() {
    startTransition(async () => setNotice(await publishVersion(versionId)));
  }

  return (
    <div className="cfgform" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {notice ? (
        notice.ok ? (
          <div className="note">
            <span className="tag t-ok">ok</span> {notice.message}
            {notice.code ? <span className="muted">&nbsp;[{notice.code}]</span> : null}
          </div>
        ) : (
          <div className="alert warn">
            <AlertTriangle className="ic" />
            <div>
              {notice.message}
              {notice.code ? <span className="muted">&nbsp;[{notice.code}]</span> : null}
            </div>
          </div>
        )
      ) : null}

      {/* Draft editor */}
      <section className="card">
        <div className="hd">
          <Syringe className="ic" />
          <h3>Draft editor — vaccination schedule</h3>
          <span className={`tag ${sourceBacked ? "t-ok" : "t-warn"}`}>
            {sourceBacked ? "Approved · publishable" : "Draft · not source-backed · cannot publish"}
          </span>
          <div className="sp" />
          <span className="muted small">draft does not generate live work</span>
        </div>
        <div className="bd">
          <div className="grid g3">
            <div>
              <label>Protocol code</label>
              <input aria-label="Protocol code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="vaccination.enterotox" />
            </div>
            <div>
              <label>Name</label>
              <input aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Enterotoxaemia" />
            </div>
            <div>
              <label>Effective from</label>
              <input type="date" aria-label="Effective from" value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} />
            </div>
            <div>
              <label>Scope</label>
              <select aria-label="Scope" value={scopeType} onChange={(e) => setScopeType(e.target.value)}>
                <option value="tenant">tenant (company default)</option>
                <option value="park">park override</option>
              </select>
            </div>
            {scopeType === "park" ? (
              <div>
                <label>Park id</label>
                <input aria-label="Park id" value={scopeId} onChange={(e) => setScopeId(e.target.value)} placeholder="park location id" />
              </div>
            ) : null}
          </div>

          <div style={{ borderTop: "1px solid var(--line2)", marginTop: 14, paddingTop: 12 }}>
            <div className="muted small" style={{ fontWeight: 700, textTransform: "uppercase", letterSpacing: ".4px", marginBottom: 8 }}>
              Eligibility
            </div>
            <div className="grid g4">
              <div>
                <label>Stage</label>
                <input aria-label="Stage" value={stage} onChange={(e) => setStage(e.target.value)} placeholder="K1 (blank = any)" />
              </div>
              <div>
                <label>Sex</label>
                <input aria-label="Sex" value={sex} onChange={(e) => setSex(e.target.value)} placeholder="any" />
              </div>
              <div>
                <label>Breed</label>
                <input aria-label="Breed" value={breed} onChange={(e) => setBreed(e.target.value)} placeholder="any" />
              </div>
              <div>
                <label>Park (eligibility)</label>
                <input aria-label="Park (eligibility)" value={parkId} onChange={(e) => setParkId(e.target.value)} placeholder="park id (blank = all)" />
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Schedule[] multi-dose rows */}
      <section className="card">
        <div className="hd">
          <CalendarDays className="ic" />
          <h3>Schedule — doses</h3>
          <span className="tag t-mut">{doses.length}</span>
          <div className="sp" />
          <button type="button" className="btn sm" onClick={() => setDoses((r) => [...r, newDose(r.length + 1)])}>
            + Add dose
          </button>
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Dose schedule rows">
          <table>
            <thead>
              <tr>
                <th>Dose code</th>
                <th>Seq</th>
                <th>Trigger</th>
                <th>Offset (d)</th>
                <th>Window (d)</th>
                <th>Min gap (d)</th>
                <th>Repeat</th>
                <th>Catch-up</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {doses.map((d, i) => (
                <tr key={i}>
                  <td>
                    <input aria-label="Dose code" value={d.doseCode} onChange={(e) => setDose(i, { doseCode: e.target.value })} placeholder="primary" />
                  </td>
                  <td style={{ width: 76 }}>
                    <input aria-label="Sequence" type="number" value={d.sequence} onChange={(e) => setDose(i, { sequence: Number(e.target.value) })} />
                  </td>
                  <td>
                    <select aria-label="Trigger type" value={d.triggerType} onChange={(e) => setDose(i, { triggerType: e.target.value })}>
                      {triggerTypes.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td style={{ width: 92 }}>
                    <input aria-label="Offset days" type="number" value={d.offsetDays} onChange={(e) => setDose(i, { offsetDays: Number(e.target.value) })} />
                  </td>
                  <td style={{ width: 92 }}>
                    <input aria-label="Due window days" type="number" value={d.dueWindowDays} onChange={(e) => setDose(i, { dueWindowDays: Number(e.target.value) })} />
                  </td>
                  <td style={{ width: 92 }}>
                    <input aria-label="Min gap days" type="number" value={d.minGapDays} onChange={(e) => setDose(i, { minGapDays: Number(e.target.value) })} />
                  </td>
                  <td>
                    <select aria-label="Repeat" value={d.repeat} onChange={(e) => setDose(i, { repeat: e.target.value })}>
                      {repeats.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <select aria-label="Catch up" value={d.catchUp} onChange={(e) => setDose(i, { catchUp: e.target.value })}>
                      {catchUps.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
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
                      ✕
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Source / review state (the publish-gate inputs) */}
      <section className="card">
        <div className="hd">
          <ShieldCheck className="ic" />
          <h3>Source &amp; review</h3>
          <div className="sp" />
          <span className="muted small">only vaccinations_db / phc / vet · approved · with approver can publish</span>
        </div>
        <div className="bd">
          <div className="grid g4">
            <div>
              <label>Source system</label>
              <select aria-label="Source system" value={sourceSystem} onChange={(e) => setSourceSystem(e.target.value)}>
                {sourceSystems.map((s) => (
                  <option key={s} value={s}>
                    {s || "—"}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label>Source ref</label>
              <input aria-label="Source ref" value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} placeholder="PHC §6.2" />
            </div>
            <div>
              <label>Review status</label>
              <select aria-label="Review status" value={reviewStatus} onChange={(e) => setReviewStatus(e.target.value)}>
                <option value="draft">draft</option>
                <option value="reviewed">reviewed</option>
                <option value="approved">approved</option>
              </select>
            </div>
            <div>
              <label>Approved by</label>
              <input aria-label="Approved by" value={approvedBy} onChange={(e) => setApprovedBy(e.target.value)} placeholder="approver name/id" />
            </div>
          </div>
        </div>
      </section>

      {/* Live impact preview */}
      <section className="card">
        <div className="hd">
          <TrendingUp className="ic" />
          <h3>Impact preview</h3>
          <div className="sp" />
          <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input
              aria-label="Vaccine item id"
              value={vaccineItemId}
              onChange={(e) => setVaccineItemId(e.target.value)}
              placeholder="vaccine item id (stock)"
              style={{ width: 180 }}
            />
            <input
              aria-label="Doses per goat"
              type="number"
              value={dosesPerGoat}
              onChange={(e) => setDosesPerGoat(Number(e.target.value))}
              style={{ width: 90 }}
            />
            <button type="button" className="btn sm" onClick={preview} disabled={pending}>
              {pending ? "Computing…" : "Preview impact"}
            </button>
          </span>
        </div>
        <div className="bd">
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
              <div className="muted small" style={{ marginTop: 12 }}>
                doses available: <b>{impact.doses_available || "—"}</b>
                {impact.earliest_expiry ? ` · earliest expiry ${impact.earliest_expiry.slice(0, 10)}` : ""}
              </div>
              {impact.warnings.length > 0 ? (
                <div style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 8 }}>
                  {impact.warnings.map((w, i) => (
                    <div key={i} className="alert warn">
                      <AlertTriangle className="ic" />
                      <div>{w}</div>
                    </div>
                  ))}
                </div>
              ) : null}
            </>
          ) : (
            <p className="muted small">Run a preview to compute live eligible goats, obligations, drive batches, and doses-vs-stock.</p>
          )}
        </div>
      </section>

      {/* Publish gate */}
      <section className="card">
        <div className="hd">
          <CheckCircle2 className="ic" />
          <h3>Publish</h3>
        </div>
        <div className="bd">
          <p className="muted small" style={{ lineHeight: 1.6 }}>
            Published versions are immutable — a change creates a new version. Drafts do not generate obligations. A
            draft that is not source-backed cannot be published; only vaccinations_db / phc / vet values reviewed and
            approved can publish, after which obligations generate from this version.
          </p>
          <div style={{ marginTop: 12, display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center" }}>
            <button type="button" className="btn" onClick={save} disabled={pending}>
              {pending ? "Saving…" : "Save draft"}
            </button>
            <button type="button" className="btn p" onClick={publish} disabled={pending || !versionId} style={!versionId ? { opacity: 0.5 } : undefined}>
              Publish
            </button>
            {versionId ? (
              <span className="muted small">draft version saved · {versionId.slice(0, 8)}</span>
            ) : (
              <span className="muted small">save the draft to enable publish</span>
            )}
          </div>
        </div>
      </section>
    </div>
  );
}
