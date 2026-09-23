"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, useTransition } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { HealthDiagnosisStageRoute, HealthDiagnosisType } from "@/lib/api/server";

import { deleteDiagnosisRoute, saveDiagnosisRoute, saveDiagnosisType } from "./health-type-actions";
import { mintKey } from "./health-register-keys";

/**
 * The write controls for diagnosis types and their routing.
 *
 * They are BUTTONS opening an in-place form, never links: every one of these writes, and the
 * navigation afterwards happens here in the client once the action has returned. A `redirect()`
 * inside a server action invoked from an event handler is swallowed by the transition, which is
 * the "Edit does nothing" bug the register half already hit.
 *
 * A confirm is two buttons where the action was, never `window.confirm` -- the interaction rules
 * ban the browser's "127.0.0.1 says" box, and a destructive routing change deserves a sentence
 * about its consequence rather than a generic OK/Cancel.
 */

// One key per human INTENT, minted on the first press and reused across retries of that same
// press, then rotated once it succeeds. A network-failed write is then safe to press again: the
// replay returns what was already written instead of writing twice.
function useIntentKey(prefix: string) {
  const key = useRef<string>("");
  return {
    take: () => {
      if (!key.current) key.current = mintKey(prefix);
      return key.current;
    },
    rotate: () => {
      key.current = "";
    },
  };
}

function Feedback({ error }: { error: string }) {
  if (!error) return null;
  return (
    <div className="small" style={{ color: "var(--danger)", marginTop: 6, lineHeight: 1.5 }}>
      {error}
    </div>
  );
}

export function DiagnosisTypeControls({
  pageContract,
  mode,
  type,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  mode: "create" | "edit";
  type?: HealthDiagnosisType;
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [confirmRetire, setConfirmRetire] = useState(false);
  const [label, setLabel] = useState(type?.label ?? "");
  const [typeKey, setTypeKey] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const intent = useIntentKey(mode === "create" ? "diagnosis-type-create" : `diagnosis-type-${type?.type_key}`);

  const submit = (status?: string) =>
    startTransition(async () => {
      setError("");
      const result = await saveDiagnosisType(
        {
          typeKey: mode === "create" ? typeKey : (type?.type_key ?? ""),
          label: label.trim() || (type?.label ?? ""),
          status,
          sortOrder: type?.sort_order,
        },
        intent.take(),
      );
      if (!result.ok) {
        setError(result.detail ?? copy(pageContract, "action.error_backend"));
        return;
      }
      intent.rotate();
      setOpen(false);
      setConfirmRetire(false);
      setTypeKey("");
      router.refresh();
    });

  if (!open) {
    return (
      <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
        <button
          type="button"
          className="btn ghost"
          disabled={!enabled || pending}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
        >
          {mode === "create" ? (
            <>
              <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_type")}
            </>
          ) : (
            <>
              <Pencil className="ic" aria-hidden="true" /> {copy(pageContract, "action.edit_type")}
            </>
          )}
        </button>
        {mode === "edit" && type && !type.is_builtin && type.status === "active" ? (
          confirmRetire ? (
            <>
              {/* The consequence, then the two buttons, exactly where the action was. */}
              <span className="small muted" style={{ maxWidth: 260, lineHeight: 1.5 }}>
                {type.route_count > 0
                  ? copy(pageContract, "warn.retire_routed")
                  : copy(pageContract, "action.retire_type")}
              </span>
              <button type="button" className="btn" disabled={pending} onClick={() => submit("retired")}>
                {copy(pageContract, "action.retire_type")}
              </button>
              <button type="button" className="btn ghost" disabled={pending} onClick={() => setConfirmRetire(false)}>
                {copy(pageContract, "action.cancel")}
              </button>
            </>
          ) : (
            <button
              type="button"
              className="btn ghost"
              disabled={!enabled || pending}
              title={!enabled ? disabledReason : ""}
              onClick={() => setConfirmRetire(true)}
            >
              {copy(pageContract, "action.retire_type")}
            </button>
          )
        ) : null}
        <Feedback error={error} />
      </div>
    );
  }

  return (
    <div style={{ display: "flex", gap: 6, alignItems: "flex-start", flexWrap: "wrap" }}>
      <div>
        <input
          className="input"
          value={label}
          placeholder={copy(pageContract, "label.type_label")}
          onChange={(e) => setLabel(e.target.value)}
          aria-label={copy(pageContract, "label.type_label")}
        />
        {mode === "create" ? (
          <>
            <input
              className="input"
              style={{ marginTop: 6 }}
              value={typeKey}
              placeholder={copy(pageContract, "label.type_key")}
              onChange={(e) => setTypeKey(e.target.value)}
              aria-label={copy(pageContract, "label.type_key")}
            />
            {/* The key is permanent and the name is not. Saying so before the first save is
                cheaper than explaining afterwards why a rename did not move it. */}
            <div className="small muted" style={{ marginTop: 4, maxWidth: 280, lineHeight: 1.5 }}>
              {copy(pageContract, "note.type_key_fixed")}
            </div>
          </>
        ) : null}
        <Feedback error={error} />
      </div>
      <button type="button" className="btn primary" disabled={pending} onClick={() => submit(type?.status)}>
        {copy(pageContract, "action.save")}
      </button>
      <button type="button" className="btn ghost" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </button>
    </div>
  );
}

export function RouteControls({
  pageContract,
  mode,
  route,
  types,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  mode: "create" | "edit";
  route?: HealthDiagnosisStageRoute;
  types: HealthDiagnosisType[];
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [ageBand, setAgeBand] = useState(route?.age_band ?? "kid");
  const [stageCode, setStageCode] = useState(route?.stage_code ?? "");
  const [typeKey, setTypeKey] = useState(route?.type_key ?? types[0]?.type_key ?? "");
  const [subStage, setSubStage] = useState(route?.sub_stage ?? "");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const intent = useIntentKey(`diagnosis-route-${route?.age_band ?? "new"}-${route?.stage_code ?? ""}`);

  const save = () =>
    startTransition(async () => {
      setError("");
      const result = await saveDiagnosisRoute(
        { ageBand, stageCode, typeKey, subStage },
        intent.take(),
      );
      if (!result.ok) {
        setError(result.detail ?? copy(pageContract, "action.error_backend"));
        return;
      }
      intent.rotate();
      setOpen(false);
      router.refresh();
    });

  const remove = () =>
    startTransition(async () => {
      setError("");
      const result = await deleteDiagnosisRoute(
        { ageBand: route?.age_band ?? ageBand, stageCode: route?.stage_code ?? stageCode },
        intent.take(),
      );
      if (!result.ok) {
        setError(result.detail ?? copy(pageContract, "action.error_backend"));
        return;
      }
      intent.rotate();
      setConfirmRemove(false);
      router.refresh();
    });

  if (!open) {
    return (
      <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
        <button
          type="button"
          className="btn ghost"
          disabled={!enabled || pending}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
        >
          {mode === "create" ? (
            <>
              <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_route")}
            </>
          ) : (
            <>
              <Pencil className="ic" aria-hidden="true" /> {copy(pageContract, "action.edit_route")}
            </>
          )}
        </button>
        {mode === "edit" && route ? (
          confirmRemove ? (
            <>
              <span className="small muted" style={{ maxWidth: 260, lineHeight: 1.5 }}>
                {route.is_wildcard
                  ? copy(pageContract, "note.wildcard_route")
                  : copy(pageContract, "action.route_removed")}
              </span>
              <button type="button" className="btn" disabled={pending} onClick={remove}>
                {copy(pageContract, "action.remove_route")}
              </button>
              <button type="button" className="btn ghost" disabled={pending} onClick={() => setConfirmRemove(false)}>
                {copy(pageContract, "action.cancel")}
              </button>
            </>
          ) : (
            <button
              type="button"
              className="btn ghost"
              disabled={!enabled || pending}
              title={!enabled ? disabledReason : ""}
              onClick={() => setConfirmRemove(true)}
            >
              <Trash2 className="ic" aria-hidden="true" /> {copy(pageContract, "action.remove_route")}
            </button>
          )
        ) : null}
        <Feedback error={error} />
      </div>
    );
  }

  return (
    <div style={{ display: "flex", gap: 6, alignItems: "flex-start", flexWrap: "wrap" }}>
      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
        <select
          className="input"
          value={ageBand}
          disabled={mode === "edit"}
          onChange={(e) => setAgeBand(e.target.value as "adult" | "kid")}
          aria-label={copy(pageContract, "label.band.adult")}
        >
          <option value="adult">{copy(pageContract, "label.band.adult")}</option>
          <option value="kid">{copy(pageContract, "label.band.kid")}</option>
        </select>
        <input
          className="input"
          value={stageCode}
          disabled={mode === "edit"}
          placeholder={copy(pageContract, "label.stage")}
          onChange={(e) => setStageCode(e.target.value)}
          aria-label={copy(pageContract, "label.stage")}
        />
        <select
          className="input"
          value={typeKey}
          onChange={(e) => setTypeKey(e.target.value)}
          aria-label={copy(pageContract, "section.types.title")}
        >
          {types.map((t) => (
            <option key={t.type_key} value={t.type_key}>
              {t.label}
            </option>
          ))}
        </select>
        <input
          className="input"
          value={subStage}
          placeholder={copy(pageContract, "label.sub_stage")}
          onChange={(e) => setSubStage(e.target.value)}
          aria-label={copy(pageContract, "label.sub_stage")}
        />
      </div>
      <button type="button" className="btn primary" disabled={pending} onClick={save}>
        {copy(pageContract, "action.save")}
      </button>
      <button type="button" className="btn ghost" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </button>
      <Feedback error={error} />
    </div>
  );
}

/**
 * The one-press fix for a gap: the stage is already known, so the only question is which type.
 *
 * It exists because the gap table is where a director LOOKS when something is wrong, and making
 * them retype a stage code they are staring at is how a fix gets postponed.
 */
export function MapStageButton({
  pageContract,
  ageBand,
  stageCode,
  stageLabel,
  types,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  ageBand: string;
  stageCode: string;
  stageLabel: string;
  types: HealthDiagnosisType[];
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [typeKey, setTypeKey] = useState(types[0]?.type_key ?? "");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const intent = useIntentKey(`diagnosis-gap-${ageBand}-${stageCode}`);

  if (!open) {
    return (
      <div>
        <button
          type="button"
          className="btn"
          disabled={!enabled || pending || types.length === 0}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
          aria-label={`${copy(pageContract, "action.map_stage")} — ${stageLabel}`}
        >
          {copy(pageContract, "action.map_stage")}
        </button>
        <Feedback error={error} />
      </div>
    );
  }

  return (
    <div style={{ display: "flex", gap: 6, alignItems: "flex-start", flexWrap: "wrap" }}>
      <select
        className="input"
        value={typeKey}
        onChange={(e) => setTypeKey(e.target.value)}
        aria-label={copy(pageContract, "section.types.title")}
      >
        {types.map((t) => (
          <option key={t.type_key} value={t.type_key}>
            {t.label}
          </option>
        ))}
      </select>
      <button
        type="button"
        className="btn primary"
        disabled={pending}
        onClick={() =>
          startTransition(async () => {
            setError("");
            const result = await saveDiagnosisRoute({ ageBand, stageCode, typeKey }, intent.take());
            if (!result.ok) {
              setError(result.detail ?? copy(pageContract, "action.error_backend"));
              return;
            }
            intent.rotate();
            setOpen(false);
            router.refresh();
          })
        }
      >
        {copy(pageContract, "action.save")}
      </button>
      <button type="button" className="btn ghost" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </button>
      <Feedback error={error} />
    </div>
  );
}
