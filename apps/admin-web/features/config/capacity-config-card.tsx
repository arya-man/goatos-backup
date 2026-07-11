"use client";

import { useState, useTransition } from "react";
import { Gauge } from "lucide-react";
import { Tag, InfoTooltip } from "@/components/ui-primitives";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { VaccinationCapacityConfig } from "@/lib/api/server";
import { saveCapacityConfig } from "./config-actions";

// Daily vaccination capacity authoring, inside the Config → vaccination flow. Reads the tenant cap (from
// the server-rendered page) and PUTs edits with optimistic concurrency. The cap counts vaccination cells,
// not animals — the info tooltip + backend labels say so; no cap value is hardcoded here (initial comes
// from the backend, options from the page contract). Only 'tenant' scope is honored today; center/shed
// render disabled-with-reason rather than as live-but-ignored choices.
export function CapacityConfigCard({
  initial,
  pageContract,
}: {
  initial: VaccinationCapacityConfig;
  pageContract: AdminUiPageContract;
}) {
  const scopeOptions = optionGroup(pageContract, "capacity_scopes");
  const overflowOptions = optionGroup(pageContract, "capacity_overflow_policies");

  const [maxPerDay, setMaxPerDay] = useState(String(initial.maxPerDay));
  const [capacityScope, setCapacityScope] = useState<string>(initial.capacityScope);
  const [maxBufferDays, setMaxBufferDays] = useState(String(initial.maxBufferDays));
  const [overflowPolicy, setOverflowPolicy] = useState<string>(initial.overflowPolicy);
  const [rowVersion, setRowVersion] = useState(initial.rowVersion);
  const [status, setStatus] = useState<{ tone: "ok" | "dng"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();

  function onSave() {
    setStatus(null);
    startTransition(async () => {
      const res = await saveCapacityConfig({
        maxPerDay: Number(maxPerDay),
        capacityScope,
        maxBufferDays: Number(maxBufferDays),
        overflowPolicy,
        expectedRowVersion: rowVersion,
      });
      if (res.ok && res.config) {
        setRowVersion(res.config.rowVersion);
        setMaxPerDay(String(res.config.maxPerDay));
        setCapacityScope(res.config.capacityScope);
        setMaxBufferDays(String(res.config.maxBufferDays));
        setOverflowPolicy(res.config.overflowPolicy);
        setStatus({ tone: "ok", text: copy(pageContract, "capacity.saved") });
        return;
      }
      const text =
        res.code === "stale_row_version"
          ? copy(pageContract, "capacity.stale")
          : `${copy(pageContract, "capacity.error_prefix")} ${res.message}`;
      setStatus({ tone: "dng", text });
    });
  }

  return (
    <section className="card" style={{ marginBottom: 14 }}>
      <div className="hd">
        <Gauge className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3 style={{ display: "inline-flex", alignItems: "center" }}>
          {copy(pageContract, "capacity.title")}
          <InfoTooltip label={copy(pageContract, "capacity.info.label")}>
            {copy(pageContract, "capacity.info")}
          </InfoTooltip>
        </h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "capacity.note")}</span>
      </div>
      <div className="bd">
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(190px,1fr))",
            gap: 14,
            maxWidth: 960,
            alignItems: "start",
          }}
        >
          <div className="fld" style={{ marginBottom: 0 }}>
            <label>{copy(pageContract, "capacity.field.max_per_day")}</label>
            <input
              type="number"
              min={1}
              inputMode="numeric"
              value={maxPerDay}
              placeholder={copy(pageContract, "capacity.field.max_per_day_placeholder")}
              onChange={(e) => setMaxPerDay(e.target.value)}
              aria-label={copy(pageContract, "capacity.field.max_per_day")}
            />
          </div>
          <div className="fld" style={{ marginBottom: 0 }}>
            <label>{copy(pageContract, "capacity.field.max_buffer_days")}</label>
            <input
              type="number"
              min={0}
              inputMode="numeric"
              value={maxBufferDays}
              placeholder={copy(pageContract, "capacity.field.max_buffer_days_placeholder")}
              onChange={(e) => setMaxBufferDays(e.target.value)}
              aria-label={copy(pageContract, "capacity.field.max_buffer_days")}
            />
            <div className="muted small" style={{ marginTop: 6, lineHeight: 1.4 }}>
              {copy(pageContract, "capacity.help.max_buffer_days")}
            </div>
          </div>
          <div className="fld" style={{ marginBottom: 0 }}>
            <label>{copy(pageContract, "capacity.field.capacity_scope")}</label>
            <select
              value={capacityScope}
              onChange={(e) => setCapacityScope(e.target.value)}
              aria-label={copy(pageContract, "capacity.field.capacity_scope")}
            >
              {scopeOptions.map((o) => (
                <option key={o.key} value={o.key} disabled={!o.enabled} title={o.enabled ? undefined : o.disabled_reason}>
                  {o.label}
                  {o.enabled ? "" : ` — ${o.disabled_reason}`}
                </option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ marginBottom: 0 }}>
            <label>{copy(pageContract, "capacity.field.overflow_policy")}</label>
            <select
              value={overflowPolicy}
              onChange={(e) => setOverflowPolicy(e.target.value)}
              aria-label={copy(pageContract, "capacity.field.overflow_policy")}
            >
              {overflowOptions.map((o) => (
                <option key={o.key} value={o.key}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginTop: 14, flexWrap: "wrap" }}>
          <button type="button" className="btn p" onClick={onSave} disabled={pending}>
            {pending ? copy(pageContract, "capacity.action.saving") : copy(pageContract, "capacity.action.save")}
          </button>
          {status ? <Tag tone={status.tone}>{status.text}</Tag> : null}
        </div>
      </div>
    </section>
  );
}
