"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, useTransition } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import ListSubheader from "@mui/material/ListSubheader";
import MenuItem from "@mui/material/MenuItem";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { Iconify } from "@/components/minimal/iconify";

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
    <Typography variant="body2" component="div" sx={{ color: "error.main", mt: 0.75 }}>
      {error}
    </Typography>
  );
}

/** The in-place confirm sentence: the consequence, beside the two buttons that replace the action. */
function ConfirmNote({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" component="span" sx={{ color: "text.secondary", maxWidth: 260 }}>
      {children}
    </Typography>
  );
}

/** A row of in-place controls (template small Buttons / fields), wrapping on a phone. */
function ControlRow({ align = "center", children }: { align?: "center" | "flex-start"; children: React.ReactNode }) {
  return (
    <Stack direction="row" spacing={0.75} useFlexGap sx={{ alignItems: align, flexWrap: "wrap" }}>
      {children}
    </Stack>
  );
}

const FIELD_SX = { minWidth: 160 } as const;

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
      <ControlRow>
        <Button
          type="button"
          variant="outlined"
          color="inherit"
          size="small"
          disabled={!enabled || pending}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
          startIcon={<Iconify icon={mode === "create" ? "mingcute:add-line" : "solar:pen-bold"} aria-hidden="true" />}
        >
          {mode === "create" ? copy(pageContract, "action.add_type") : copy(pageContract, "action.edit_type")}
        </Button>
        {mode === "edit" && type && !type.is_builtin && type.status === "active" ? (
          confirmRetire ? (
            <>
              {/* The consequence, then the two buttons, exactly where the action was. */}
              <ConfirmNote>
                {type.route_count > 0
                  ? copy(pageContract, "warn.retire_routed")
                  : copy(pageContract, "action.retire_type")}
              </ConfirmNote>
              <Button type="button" variant="soft" color="error" size="small" disabled={pending} onClick={() => submit("retired")}>
                {copy(pageContract, "action.retire_type")}
              </Button>
              <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => setConfirmRetire(false)}>
                {copy(pageContract, "action.cancel")}
              </Button>
            </>
          ) : (
            <Button
              type="button"
              variant="outlined"
              color="inherit"
              size="small"
              disabled={!enabled || pending}
              title={!enabled ? disabledReason : ""}
              onClick={() => setConfirmRetire(true)}
            >
              {copy(pageContract, "action.retire_type")}
            </Button>
          )
        ) : null}
        <Feedback error={error} />
      </ControlRow>
    );
  }

  return (
    <ControlRow align="flex-start">
      <Stack spacing={0.75}>
        <TextField
          size="small"
          label={copy(pageContract, "label.type_label")}
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          sx={FIELD_SX}
        />
        {mode === "create" ? (
          <TextField
            size="small"
            label={copy(pageContract, "label.type_key")}
            value={typeKey}
            onChange={(e) => setTypeKey(e.target.value)}
            // The key is permanent and the name is not. Saying so before the first save is
            // cheaper than explaining afterwards why a rename did not move it.
            helperText={copy(pageContract, "note.type_key_fixed")}
            sx={[FIELD_SX, { maxWidth: 280 }]}
          />
        ) : null}
        <Feedback error={error} />
      </Stack>
      <Button type="button" variant="contained" color="primary" size="small" disabled={pending} onClick={() => submit(type?.status)}>
        {copy(pageContract, "action.save")}
      </Button>
      <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </Button>
    </ControlRow>
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
      <ControlRow>
        <Button
          type="button"
          variant="outlined"
          color="inherit"
          size="small"
          disabled={!enabled || pending}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
          startIcon={<Iconify icon={mode === "create" ? "mingcute:add-line" : "solar:pen-bold"} aria-hidden="true" />}
        >
          {mode === "create" ? copy(pageContract, "action.add_route") : copy(pageContract, "action.edit_route")}
        </Button>
        {mode === "edit" && route ? (
          confirmRemove ? (
            <>
              <ConfirmNote>
                {route.is_wildcard
                  ? copy(pageContract, "note.wildcard_route")
                  : copy(pageContract, "action.route_removed")}
              </ConfirmNote>
              <Button type="button" variant="soft" color="error" size="small" disabled={pending} onClick={remove}>
                {copy(pageContract, "action.remove_route")}
              </Button>
              <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => setConfirmRemove(false)}>
                {copy(pageContract, "action.cancel")}
              </Button>
            </>
          ) : (
            <Button
              type="button"
              variant="outlined"
              color="inherit"
              size="small"
              disabled={!enabled || pending}
              title={!enabled ? disabledReason : ""}
              onClick={() => setConfirmRemove(true)}
              startIcon={<Iconify icon="solar:trash-bin-trash-bold" aria-hidden="true" />}
            >
              {copy(pageContract, "action.remove_route")}
            </Button>
          )
        ) : null}
        <Feedback error={error} />
      </ControlRow>
    );
  }

  return (
    <ControlRow align="flex-start">
      <ControlRow align="flex-start">
        <TextField
          select
          size="small"
          label={copy(pageContract, "label.band.adult")}
          value={ageBand}
          disabled={mode === "edit"}
          onChange={(e) => setAgeBand(e.target.value as "adult" | "kid")}
          sx={FIELD_SX}
        >
          <MenuItem value="adult">{copy(pageContract, "label.band.adult")}</MenuItem>
          <MenuItem value="kid">{copy(pageContract, "label.band.kid")}</MenuItem>
        </TextField>
        {/* PICKED, NEVER TYPED. A typed stage code that matches no animal makes a route that can
            never fire, and its only symptom is a zero in a column: on 2026-09-23 `mothers` was
            typed where the farm's stage is `Mother`, the screen accepted it, and five does stayed
            on the adult wildcard behind a rule that looked authored. The backend refuses an
            unknown stage now too -- this is the half that stops it being typed at all. */}
        <TextField
          select
          size="small"
          label={copy(pageContract, "label.stage")}
          value={stageCode}
          disabled={mode === "edit"}
          onChange={(e) => setStageCode(e.target.value)}
          sx={FIELD_SX}
        >
          <MenuItem value="">{copy(pageContract, "label.stage")}</MenuItem>
          <MenuItem value="*">{copy(pageContract, "label.every_stage")}</MenuItem>
          {stages
            .filter((s) => s.age_band === ageBand)
            .map((s) => (
              <MenuItem key={s.stage_code} value={s.stage_code}>
                {s.stage_label || s.stage_code} ({s.live_animals})
              </MenuItem>
            ))}
        </TextField>
        <TextField
          select
          size="small"
          label={copy(pageContract, "section.types.title")}
          value={typeKey}
          onChange={(e) => setTypeKey(e.target.value)}
          sx={FIELD_SX}
        >
          {types.map((t) => (
            <MenuItem key={t.type_key} value={t.type_key}>
              {t.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          size="small"
          label={copy(pageContract, "label.sub_stage")}
          value={subStage}
          onChange={(e) => setSubStage(e.target.value)}
          sx={FIELD_SX}
        />
      </ControlRow>
      <Button type="button" variant="contained" color="primary" size="small" disabled={pending} onClick={save}>
        {copy(pageContract, "action.save")}
      </Button>
      <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </Button>
      <Feedback error={error} />
    </ControlRow>
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
        <Button
          type="button"
          variant="soft"
          color="primary"
          size="small"
          disabled={!enabled || pending || types.length === 0}
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
          aria-label={`${copy(pageContract, "action.map_stage")} — ${stageLabel}`}
        >
          {copy(pageContract, "action.map_stage")}
        </Button>
        <Feedback error={error} />
      </div>
    );
  }

  return (
    <ControlRow align="flex-start">
      <TextField
        select
        size="small"
        label={copy(pageContract, "section.types.title")}
        value={typeKey}
        onChange={(e) => setTypeKey(e.target.value)}
        sx={FIELD_SX}
      >
        {types.map((t) => (
          <MenuItem key={t.type_key} value={t.type_key}>
            {t.label}
          </MenuItem>
        ))}
      </TextField>
      <Button
        type="button"
        variant="contained"
        color="primary"
        size="small"
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
      </Button>
      <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </Button>
      <Feedback error={error} />
    </ControlRow>
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
      <Stack spacing={0.5} sx={{ display: "inline-flex" }}>
        <Button
          type="button"
          variant="outlined"
          color="inherit"
          size="small"
          startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />}
          disabled={!enabled || pending || offer.length === 0}
          // offer.length is 0 only when the farm has NO stages at all, which is a real state on a
          // tenant whose catalog was never seeded -- and then there is genuinely nothing to add.
          title={!enabled ? disabledReason : ""}
          onClick={() => setOpen(true)}
        >
          {copy(pageContract, "action.add_stage_to_type")}
        </Button>
        <Feedback error={error} />
      </Stack>
    );
  }

  return (
    <ControlRow>
      <TextField
        select
        size="small"
        label={copy(pageContract, "label.stage")}
        value={choice}
        onChange={(e) => setChoice(e.target.value)}
        sx={FIELD_SX}
      >
        <MenuItem value="">{copy(pageContract, "label.stage")}</MenuItem>
        {/* MUI Select reads its options as direct children, so each band is a ListSubheader
            followed by its options in one flat list (was a native optgroup). */}
        {["adult", "kid"].flatMap((band) => {
          const inBand = offer.filter((s) => s.age_band === band);
          if (inBand.length === 0) return [];
          return [
            <ListSubheader key={`band-${band}`}>
              {band === "adult"
                ? copy(pageContract, "label.band.adult")
                : copy(pageContract, "label.band.kid")}
            </ListSubheader>,
            ...inBand.map((s) => (
              <MenuItem key={`${s.age_band}/${s.stage_code}`} value={`${s.age_band}/${s.stage_code}`}>
                {/* Where it is NOW, so a move is never a surprise. */}
                {s.stage_label || s.stage_code} ({s.live_animals})
                {s.routed_type_label && s.routed_type_key !== typeKey
                  ? ` — ${s.routed_type_label}`
                  : ""}
              </MenuItem>
            )),
          ];
        })}
      </TextField>
      <Button
        type="button"
        variant="contained"
        color="primary"
        size="small"
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
      </Button>
      <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending} onClick={() => { setOpen(false); setError(""); }}>
        {copy(pageContract, "action.cancel")}
      </Button>
      <Feedback error={error} />
    </ControlRow>
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
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, flexWrap: "wrap" }}>
      <Chip
        size="small"
        variant="soft"
        label={label}
        title={!enabled ? disabledReason : undefined}
        // The delete affordance opens the in-place confirm; while confirming, the two buttons
        // beside the chip are the action.
        onDelete={enabled && !pending && !confirming ? () => setConfirming(true) : undefined}
        deleteIcon={
          <Iconify icon="solar:trash-bin-trash-bold" aria-label={`${copy(pageContract, "action.remove_route")} — ${label}`} />
        }
      />
      {confirming ? (
        <>
          <Button
            type="button"
            variant="soft"
            color="error"
            size="small"
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
          </Button>
          <Button type="button" variant="outlined" color="inherit" size="small" disabled={pending}
            onClick={() => setConfirming(false)}>
            {copy(pageContract, "action.cancel")}
          </Button>
        </>
      ) : null}
      <Feedback error={error} />
    </Box>
  );
}
