"use client";

import { useState, useTransition } from "react";
import {
  publishVersion,
  runImpactPreview,
  saveDraft,
  type ActionResult,
  type DoseRow,
} from "../config-actions";
import type { ImpactPreviewResult } from "@/lib/api/server";

const input =
  "min-h-[40px] w-full rounded-md border border-[#334155] bg-[#0f1115] px-2.5 py-2 text-sm text-[#c7d1dc] focus:border-[#14f1d9]/60 focus:outline-none";
const label = "mb-1 block text-xs text-[#93a4b8]";
const card = "rounded-xl border border-[#334155] bg-[#1A1D24]";
const cardHd = "flex items-center gap-3 border-b border-[#334155] px-4 py-3";
const btn = "inline-flex min-h-[40px] items-center rounded-md border px-3 py-2 text-sm font-medium";
const btnMut = `${btn} border-[#334155] text-[#c7d1dc] hover:border-[#14f1d9]/40`;
const btnPrimary = `${btn} border-[#14f1d9] bg-[rgba(20,241,217,0.1)] text-[#14f1d9] disabled:opacity-50`;

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
    <div className="flex flex-col gap-4">
      {notice ? (
        <div
          className={`rounded-md border px-4 py-2.5 text-sm ${
            notice.ok ? "border-[#1f8f65] bg-[#0c1a13] text-[#7dd3a7]" : "border-[#a16207] bg-[#1f1a07] text-[#facc15]"
          }`}
        >
          {notice.message}
          {notice.code ? <span className="ml-2 opacity-70">[{notice.code}]</span> : null}
        </div>
      ) : null}

      {/* Draft editor */}
      <section className={card}>
        <div className={cardHd}>
          <span aria-hidden>💉</span>
          <h2 className="text-sm font-bold text-white">Draft editor — vaccination schedule</h2>
          <span className={`rounded-md border px-2 py-0.5 text-xs ${sourceBacked ? "border-[#1f8f65] text-[#7dd3a7]" : "border-[#a16207] text-[#facc15]"}`}>
            {sourceBacked ? "Approved · publishable" : "Draft · not source-backed · cannot publish"}
          </span>
          <span className="ml-auto text-xs text-[#8899AA]">draft does not generate live work</span>
        </div>
        <div className="grid gap-3 p-4 md:grid-cols-3">
          <div>
            <span className={label}>Protocol code</span>
            <input aria-label="Protocol code" className={input} value={code} onChange={(e) => setCode(e.target.value)} placeholder="vaccination.enterotox" />
          </div>
          <div>
            <span className={label}>Name</span>
            <input aria-label="Name" className={input} value={name} onChange={(e) => setName(e.target.value)} placeholder="Enterotoxaemia" />
          </div>
          <div>
            <span className={label}>Effective from</span>
            <input type="date" aria-label="Effective from" className={input} value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} />
          </div>
          <div>
            <span className={label}>Scope</span>
            <select aria-label="Scope" className={input} value={scopeType} onChange={(e) => setScopeType(e.target.value)}>
              <option value="tenant">tenant (company default)</option>
              <option value="park">park override</option>
            </select>
          </div>
          {scopeType === "park" ? (
            <div>
              <span className={label}>Park id</span>
              <input aria-label="Park id" className={input} value={scopeId} onChange={(e) => setScopeId(e.target.value)} placeholder="park location id" />
            </div>
          ) : null}
        </div>

        <div className="border-t border-[#334155] px-4 py-3">
          <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-[#93a4b8]">Eligibility</div>
          <div className="grid gap-3 md:grid-cols-4">
            <div>
              <span className={label}>Stage</span>
              <input aria-label="Stage" className={input} value={stage} onChange={(e) => setStage(e.target.value)} placeholder="K1 (blank = any)" />
            </div>
            <div>
              <span className={label}>Sex</span>
              <input aria-label="Sex" className={input} value={sex} onChange={(e) => setSex(e.target.value)} placeholder="any" />
            </div>
            <div>
              <span className={label}>Breed</span>
              <input aria-label="Breed" className={input} value={breed} onChange={(e) => setBreed(e.target.value)} placeholder="any" />
            </div>
            <div>
              <span className={label}>Park (eligibility)</span>
              <input aria-label="Park (eligibility)" className={input} value={parkId} onChange={(e) => setParkId(e.target.value)} placeholder="park id (blank = all)" />
            </div>
          </div>
        </div>
      </section>

      {/* Schedule[] multi-dose rows */}
      <section className={card}>
        <div className={cardHd}>
          <span aria-hidden>🗓</span>
          <h2 className="text-sm font-bold text-white">Schedule — doses</h2>
          <span className="rounded bg-[#22262E] px-1.5 py-0.5 text-[11px] text-[#c7d1dc]">{doses.length}</span>
          <button type="button" className={`${btnMut} ml-auto`} onClick={() => setDoses((r) => [...r, newDose(r.length + 1)])}>
            + Add dose
          </button>
        </div>
        <div className="overflow-auto p-4">
          <table className="w-full min-w-[820px] border-collapse text-sm">
            <thead>
              <tr className="border-b border-[#334155] text-left text-[11px] uppercase tracking-wide text-[#93a4b8]">
                <th className="px-2 py-2">Dose code</th>
                <th className="px-2 py-2">Seq</th>
                <th className="px-2 py-2">Trigger</th>
                <th className="px-2 py-2">Offset (d)</th>
                <th className="px-2 py-2">Window (d)</th>
                <th className="px-2 py-2">Min gap (d)</th>
                <th className="px-2 py-2">Repeat</th>
                <th className="px-2 py-2">Catch-up</th>
                <th className="px-2 py-2" />
              </tr>
            </thead>
            <tbody>
              {doses.map((d, i) => (
                <tr key={i} className="border-b border-[#23272f]">
                  <td className="px-2 py-2">
                    <input aria-label="Dose code" className={input} value={d.doseCode} onChange={(e) => setDose(i, { doseCode: e.target.value })} placeholder="primary" />
                  </td>
                  <td className="px-2 py-2 w-16">
                    <input aria-label="Sequence" type="number" className={input} value={d.sequence} onChange={(e) => setDose(i, { sequence: Number(e.target.value) })} />
                  </td>
                  <td className="px-2 py-2">
                    <select aria-label="Trigger type" className={input} value={d.triggerType} onChange={(e) => setDose(i, { triggerType: e.target.value })}>
                      {triggerTypes.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="px-2 py-2 w-20">
                    <input aria-label="Offset days" type="number" className={input} value={d.offsetDays} onChange={(e) => setDose(i, { offsetDays: Number(e.target.value) })} />
                  </td>
                  <td className="px-2 py-2 w-20">
                    <input aria-label="Due window days" type="number" className={input} value={d.dueWindowDays} onChange={(e) => setDose(i, { dueWindowDays: Number(e.target.value) })} />
                  </td>
                  <td className="px-2 py-2 w-20">
                    <input aria-label="Min gap days" type="number" className={input} value={d.minGapDays} onChange={(e) => setDose(i, { minGapDays: Number(e.target.value) })} />
                  </td>
                  <td className="px-2 py-2">
                    <select aria-label="Repeat" className={input} value={d.repeat} onChange={(e) => setDose(i, { repeat: e.target.value })}>
                      {repeats.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="px-2 py-2">
                    <select aria-label="Catch up" className={input} value={d.catchUp} onChange={(e) => setDose(i, { catchUp: e.target.value })}>
                      {catchUps.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="px-2 py-2">
                    <button type="button" aria-label="Remove dose" className={btnMut} onClick={() => setDoses((r) => r.filter((_, idx) => idx !== i))} disabled={doses.length === 1}>
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
      <section className={card}>
        <div className={cardHd}>
          <span aria-hidden>🔏</span>
          <h2 className="text-sm font-bold text-white">Source &amp; review</h2>
          <span className="ml-auto text-xs text-[#8899AA]">only vaccinations_db / phc / vet · approved · with approver can publish</span>
        </div>
        <div className="grid gap-3 p-4 md:grid-cols-4">
          <div>
            <span className={label}>Source system</span>
            <select aria-label="Source system" className={input} value={sourceSystem} onChange={(e) => setSourceSystem(e.target.value)}>
              {sourceSystems.map((s) => (
                <option key={s} value={s}>
                  {s || "—"}
                </option>
              ))}
            </select>
          </div>
          <div>
            <span className={label}>Source ref</span>
            <input aria-label="Source ref" className={input} value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} placeholder="PHC §6.2" />
          </div>
          <div>
            <span className={label}>Review status</span>
            <select aria-label="Review status" className={input} value={reviewStatus} onChange={(e) => setReviewStatus(e.target.value)}>
              <option value="draft">draft</option>
              <option value="reviewed">reviewed</option>
              <option value="approved">approved</option>
            </select>
          </div>
          <div>
            <span className={label}>Approved by</span>
            <input aria-label="Approved by" className={input} value={approvedBy} onChange={(e) => setApprovedBy(e.target.value)} placeholder="approver name/id" />
          </div>
        </div>
      </section>

      {/* Live impact preview */}
      <section className={card}>
        <div className={cardHd}>
          <span aria-hidden>📈</span>
          <h2 className="text-sm font-bold text-white">Impact preview</h2>
          <span className="ml-auto flex items-center gap-2">
            <input aria-label="Vaccine item id" className={`${input} w-44`} value={vaccineItemId} onChange={(e) => setVaccineItemId(e.target.value)} placeholder="vaccine item id (stock)" />
            <input aria-label="Doses per goat" type="number" className={`${input} w-24`} value={dosesPerGoat} onChange={(e) => setDosesPerGoat(Number(e.target.value))} />
            <button type="button" className={btnMut} onClick={preview} disabled={pending}>
              {pending ? "Computing…" : "Preview impact"}
            </button>
          </span>
        </div>
        <div className="p-4">
          {impact ? (
            <>
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                {[
                  ["Eligible goats", impact.eligible_goats],
                  ["Obligations / cycle", impact.obligations],
                  ["Batches (SOP tasks)", impact.batches],
                  ["Doses required", impact.doses_required],
                ].map(([l, v]) => (
                  <div key={String(l)} className="rounded-md border border-[#334155] px-3 py-2">
                    <div className="text-xs uppercase text-[#93a4b8]">{l}</div>
                    <div className="mt-1 text-lg font-semibold text-white">{String(v)}</div>
                  </div>
                ))}
              </div>
              <div className="mt-3 text-xs text-[#8899AA]">
                doses available: <b className="text-[#c7d1dc]">{impact.doses_available || "—"}</b>
                {impact.earliest_expiry ? ` · earliest expiry ${impact.earliest_expiry.slice(0, 10)}` : ""}
              </div>
              {impact.warnings.length > 0 ? (
                <ul className="mt-3 space-y-1">
                  {impact.warnings.map((w, i) => (
                    <li key={i} className="rounded-md border border-[#a16207] bg-[#1f1a07] px-3 py-1.5 text-xs text-[#facc15]">
                      ⚠ {w}
                    </li>
                  ))}
                </ul>
              ) : null}
            </>
          ) : (
            <p className="text-sm text-[#8899AA]">Run a preview to compute live eligible goats, obligations, drive batches, and doses-vs-stock.</p>
          )}
        </div>
      </section>

      {/* Publish gate */}
      <section className={card}>
        <div className={cardHd}>
          <span aria-hidden>✅</span>
          <h2 className="text-sm font-bold text-white">Publish</h2>
        </div>
        <div className="p-4">
          <p className="text-sm leading-6 text-[#8899AA]">
            Published versions are immutable — a change creates a new version. Drafts do not generate obligations. A
            draft that is not source-backed cannot be published; only vaccinations_db / phc / vet values reviewed and
            approved can publish, after which obligations generate from this version.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <button type="button" className={btnMut} onClick={save} disabled={pending}>
              {pending ? "Saving…" : "Save draft"}
            </button>
            <button type="button" className={btnPrimary} onClick={publish} disabled={pending || !versionId}>
              Publish
            </button>
            {versionId ? <span className="self-center text-xs text-[#8899AA]">draft version saved · {versionId.slice(0, 8)}</span> : <span className="self-center text-xs text-[#8899AA]">save the draft to enable publish</span>}
          </div>
        </div>
      </section>
    </div>
  );
}
