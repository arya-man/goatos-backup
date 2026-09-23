"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, useTransition } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  HealthAvailableStage,
  HealthDiagnosisStageRoute,
  HealthDiagnosisType,
} from "@/lib/api/server";

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
  stages,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  mode: "create" | "edit";
  route?: HealthDiagnosisStageRoute;
  types: HealthDiagnosisType[];
  stages: HealthAvailableStage[];
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
        {/* PICKED, NEVER TYPED. A typed stage code that matches no animal makes a route that can
            never fire, and its only symptom is a zero in a column: on 2026-09-23 `mothers` was
            typed where the farm's stage is `Mother`, the screen accepted it, and five does stayed
            on the adult wildcard behind a rule that looked authored. The backend refuses an
            unknown stage now too -- this is the half that stops it being typed at all. */}
        <select
          className="input"
          value={stageCode}
          disabled={mode === "edit"}
          onChange={(e) => setStageCode(e.target.value)}
          aria-label={copy(pageContract, "label.stage")}
        >
          <option value="">{copy(pageContract, "label.stage")}</option>
          <option value="*">{copy(pageContract, "label.every_stage")}</option>
          {stages
            .filter((s) => s.age_band === ageBand)
            .map((s) => (
              <option key={s.stage_code} value={s.stage_code}>
                {s.stage_label || s.stage_code} ({s.live_animals})
              </option>
            ))}
        </select>
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

/**
 * Assign a stage tag TO A CATEGORY.
 *
 * Maintainer, 2026-09-23: "the categories are fixed, what stages come under I will configure".
 * The routing is read category-first -- Kids on milk is K0, K1 and K2; Mothers is the Mother tag;
 * Fattening is the F2s and Warmup because the farm says so -- and this is the control that adds
 * one to the category it sits under, so the type is never chosen twice.
 *
 * EVERY stage is offered, including ones already on another type. An earlier version hid those,
 * reasoning that moving a stage is a different act from adding one -- and that made the main
 * thing this screen exists for impossible: once every kid stage was spoken for, "Add a stage" was
 * dead on every kid category, so Warmup could never be moved onto its own type. The maintainer
 * found it within a minute.
 *
 * Moving is what the farm does, so moving is what the control offers. A stage already on another
 * type says so in the option ("now in Kids fattening"), and choosing it MOVES it -- which is
 * exactly what the table's (band, stage) key does on upsert. Nothing is silent: the reader is
 * told where it is before they pick it, and the category it leaves loses the chip on the same
 * refresh.
 */
export function AddStageToType({
  pageContract,
  typeKey,
  stages,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  typeKey: string;
  stages: HealthAvailableStage[];
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const intent = useIntentKey(`diagnosis-add-stage-${typeKey}`);

  // Both bands, always. A type's band is not fixed by what is already under it -- a farm may put
  // an adult stage and a kid stage in one category if that is how it treats them, and refusing
  // that here would be this screen inventing a clinical rule nobody asked for. The option itself
  // carries the band, so the write is never ambiguous.
  const offer = stages;

  if (!open) {
    return (
      <div style={{ display: "inline-flex", flexDirection: "column", gap: 4 }}>
        <button
          type="button"
          className="btn ghost"
          disabled={!enabled || pending || offer.length === 0}
          // offer.length is 0 only when the farm has NO stages at all, which is a real state on a
          // tenant whose catalog was never seeded -- and then there is genuinely nothing to add.
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
        >
          <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_stage_to_type")}
        </button>
        <Feedback error={error} />
      </div>
    );
  }

  return (
    <div style={{ display: "inline-flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
      <select
        className="input"
        value={choice}
        onChange={(e) => setChoice(e.target.value)}
        aria-label={copy(pageContract, "label.stage")}
      >
        <option value="">{copy(pageContract, "label.stage")}</option>
        {["adult", "kid"].map((band) => {
          const inBand = offer.filter((s) => s.age_band === band);
          if (inBand.length === 0) return null;
          return (
            <optgroup
              key={band}
              label={
                band === "adult"
                  ? copy(pageContract, "label.band.adult")
                  : copy(pageContract, "label.band.kid")
              }
            >
              {inBand.map((s) => (
                <option key={`${s.age_band}/${s.stage_code}`} value={`${s.age_band}/${s.stage_code}`}>
                  {/* Where it is NOW, so a move is never a surprise. */}
                  {s.stage_label || s.stage_code} ({s.live_animals})
                  {s.routed_type_label && s.routed_type_key !== typeKey
                    ? ` — ${s.routed_type_label}`
                    : ""}
                </option>
              ))}
            </optgroup>
          );
        })}
      </select>
      <button
        type="button"
        className="btn primary"
        disabled={pending || choice === ""}
        onClick={() =>
          startTransition(async () => {
            setError("");
            const [ageBand, stageCode] = choice.split("/");
            const result = await saveDiagnosisRoute({ ageBand, stageCode, typeKey }, intent.take());
            if (!result.ok) {
              setError(result.detail ?? copy(pageContract, "action.error_backend"));
              return;
            }
            intent.rotate();
            setOpen(false);
            setChoice("");
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

/** Take one stage tag off a category. */
export function RemoveStageChip({
  pageContract,
  ageBand,
  stageCode,
  label,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  ageBand: string;
  stageCode: string;
  label: string;
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const intent = useIntentKey(`diagnosis-drop-${ageBand}-${stageCode}`);

  return (
    <span className="chip" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      {label}
      {confirming ? (
        <>
          <button
            type="button"
            className="btn ghost"
            style={{ padding: "0 6px" }}
            disabled={pending}
            onClick={() =>
              startTransition(async () => {
                setError("");
                const result = await deleteDiagnosisRoute({ ageBand, stageCode }, intent.take());
                if (!result.ok) {
                  setError(result.detail ?? copy(pageContract, "action.error_backend"));
                  return;
                }
                intent.rotate();
                setConfirming(false);
                router.refresh();
              })
            }
          >
            {copy(pageContract, "action.remove_route")}
          </button>
          <button type="button" className="btn ghost" style={{ padding: "0 6px" }} disabled={pending}
            onClick={() => setConfirming(false)}>
            {copy(pageContract, "action.cancel")}
          </button>
        </>
      ) : (
        <button
          type="button"
          className="btn ghost"
          style={{ padding: "0 4px" }}
          disabled={!enabled || pending}
          title={!enabled ? disabledReason : ""}
          aria-label={`${copy(pageContract, "action.remove_route")} — ${label}`}
          onClick={() => setConfirming(true)}
        >
          <Trash2 className="ic" aria-hidden="true" />
        </button>
      )}
      <Feedback error={error} />
    </span>
  );
}
