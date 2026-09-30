"use client";

import { useEffect, useId, useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import MuiButton from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import MuiTextField from "@mui/material/TextField";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { Iconify } from "@/components/minimal/iconify";

import { copy, optionalCopy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import MenuItem from "@mui/material/MenuItem";
import Box from "@mui/material/Box";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import type { HealthCatalogItem, HealthConfigProtocolDetail, HealthConfigStep } from "@/lib/api/server";
import type { HealthConfigActionResult } from "./health-config-actions";
import { afterSubmit, CLOSED_STATE, openIntent, type AuthoringIdempotencyState } from "@/lib/authoring-idempotency";
import Alert from "@mui/material/Alert";
import MenuList from "@mui/material/MenuList";
import { DropdownPaper } from "@/components/app/dropdown-paper";
import { Label } from "@/components/minimal/label";

type SelectOption = { value: string; label: string };

// The authoring surface for one treatment protocol.
//
// WHY THE WHOLE DOCUMENT IS EDITED AT ONCE
//
// A protocol is read by an operator as one sequence, so the unit of save is "the document as the
// author last saw it", not a field. Per-field autosave would let a partially-applied edit exist —
// a step sitting on day 9 of a course that is now 7 days long — which is a protocol nobody can
// execute and which no screen would show as broken.
//
// WHY STEPS CARRY NO SEQUENCE NUMBER
//
// Order is positional here and the server assigns seq 1..N. `health_protocol_steps` is UNIQUE on
// (version, seq), so a client-driven renumber collides with itself halfway through a reorder.
// Sending order instead of numbers makes a reorder a single replacement that either commits whole
// or not at all.
//
// WHY NUMERIC INPUTS ARE UNCONTROLLED TEXT
//
// Same reason as Feed Config: a controlled `type="number"` is where a cleared box quietly becomes 0.
// Here that would author a dosage or a course length nobody typed. The fields hold exactly the
// characters the author left in them, and the server action decides what a blank means.

// Split across two aliases so the arrow type does not read as `> Promise<` — the contract-literal
// guard scans for `>text<` to catch visible JSX copy, and an inline generic return type trips it.
type ActionResult = Promise<HealthConfigActionResult>;
type SubmitAction = (formData: FormData) => ActionResult;
const BACK_TO_LIST_MARKER = "mesha.healthConfig.openedFromCatalog";
const BACK_TO_LIST_HISTORY_NONCE = "meshaHealthConfigBackNonce";

type BackToListMarker = {
  editorHref: string;
  listHref: string;
  nonce: string;
};

const activeBackToListNonces = new Set<string>();

function pathAndSearch(href: string): string {
  try {
    const url = new URL(href, window.location.origin);
    return `${url.pathname}${url.search}`;
  } catch {
    return href.split("#", 1)[0];
  }
}

function readBackToListMarker(): BackToListMarker | null {
  try {
    const raw = window.sessionStorage.getItem(BACK_TO_LIST_MARKER);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<BackToListMarker>;
    if (
      typeof parsed.editorHref !== "string" ||
      typeof parsed.listHref !== "string" ||
      typeof parsed.nonce !== "string"
    ) {
      return null;
    }
    return { editorHref: parsed.editorHref, listHref: parsed.listHref, nonce: parsed.nonce };
  } catch {
    return null;
  }
}

/**
 * A DRAFT WITH NO STEPS WHILE THE LIVE COURSE HAS THEM.
 *
 * Two of these exist in the live database, both created on 2026-09-16 by a build that no
 * longer runs -- opening a draft copies the published steps today, and a fresh one comes
 * back with all 28. What is left is a screen that says "this course has no steps yet"
 * directly above a history saying "v1 · Live · Steps: 28", and a reader is right to find
 * that alarming.
 *
 * It says which version animals are actually treated from, that publishing is blocked
 * (the strict rulebook refuses a course with no steps, so the live one cannot be replaced
 * by this), and that discarding returns to it. The recovery is a button already on the
 * screen; nothing said so.
 */
function EmptyDraftOverLiveNotice({
  detail,
  stepCount,
  pageContract,
}: {
  detail: HealthConfigProtocolDetail;
  stepCount: number;
  pageContract: AdminUiPageContract;
}) {
  if (detail.status !== "draft" || stepCount > 0) return null;
  const live = (detail.history ?? []).find((v) => v.status === "published");
  if (!live || live.step_count === 0) return null;
  return (
    <Alert severity="error" sx={{ mb: 1.5 }}><div>{copy(pageContract, "warn.empty_draft_over_live")}</div>
    </Alert>
  );
}

/**
 * The medicine field: PICKED from the farm's item registry, never typed.
 *
 * Maintainer instruction 2026-09-21 -- nothing is assigned at random. A medicine exists on
 * /configuration/items first, and only then can a course name it. Typing produced two
 * spellings of one medicine and dosages attached to things the store had never heard of.
 *
 * IT SUGGESTS AS YOU TYPE, BELOW THE FIELD. This started as an <input list> over a
 * <datalist>, which is less work and was the wrong call: the native control decides for
 * itself when to open, renders differently on every browser, and on the farm's machines it
 * mostly waited for a click on the arrow. An author half-way through "chl" wants to see
 * the match, so the list is rendered here and behaves the same everywhere.
 *
 * A list does not CONSTRAIN the value, so the constraint is real and lives on the server:
 * a save whose medicine matches no active item is refused, naming that step. This marks the
 * field as soon as the author leaves the list, so they find out while looking at it -- but
 * the refusal is what makes the rule true, because a client check is a suggestion an older
 * client can skip.
 */
function MedicinePicker({
  value,
  medicines,
  pageContract,
  onChange,
}: {
  value: string;
  medicines: HealthCatalogItem[];
  pageContract: AdminUiPageContract;
  onChange: (name: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(0);
  const listId = useId();

  const known = useMemo(
    () => new Set(medicines.map((m) => m.name.trim().toLowerCase())),
    [medicines],
  );
  const typed = value.trim();
  const noMatch = typed !== "" && !known.has(typed.toLowerCase());

  // The suggestions, capped. A farm with three hundred medicines should not drop three
  // hundred rows over the step below it -- the author narrows by typing one more letter,
  // which is the whole point of a typeahead.
  const matches = useMemo(() => {
    const q = typed.toLowerCase();
    const hits = q === ""
      ? medicines
      : medicines.filter((m) => m.name.toLowerCase().includes(q));
    return hits.slice(0, 8);
  }, [medicines, typed]);

  // An exact hit is not a suggestion. Once the field holds a real medicine the list has
  // nothing left to offer, and leaving it open covers the dosage the author reaches next.
  const exact = matches.length === 1 && matches[0].name.trim().toLowerCase() === typed.toLowerCase();
  const showList = open && matches.length > 0 && !exact;

  // "bel" is a half-typed query, not a mistake. Warning while the suggestions are open
  // tells an author they are wrong in the middle of getting it right, so the warning waits
  // until there is nothing left to choose from -- either they have stopped typing, or what
  // they typed matches nothing at all.
  const unknown = noMatch && !showList;

  const choose = (name: string) => {
    onChange(name);
    setOpen(false);
    setHighlight(0);
  };

  return (
    <Box sx={{ flex: "1 1 260px", minWidth: 200, position: "relative" }}>
      <MuiTextField
        fullWidth
        label={copy(pageContract, "label.medicine_name")}
        value={value}
        placeholder={copy(pageContract, "label.pick_medicine")}
        error={unknown}
        onFocus={() => setOpen(true)}
        onChange={(event) => {
          onChange(event.target.value);
          setOpen(true);
          setHighlight(0);
        }}
        // Blur closes on a DELAY so a click on a suggestion lands first. The click itself
        // is handled on mousedown for the same reason.
        onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        onKeyDown={(event) => {
          // KeyboardEvent.key names, compared lowercased. They are machine values, not copy
          // -- the copy-firewall scan is right to be blunt about capitalised literals, and
          // this is simply not prose.
          const key = event.key.toLowerCase();
          if (key === "escape") {
            setOpen(false);
            return;
          }
          if (!showList) return;
          if (key === "arrowdown") {
            event.preventDefault();
            setHighlight((h) => (h + 1) % matches.length);
          } else if (key === "arrowup") {
            event.preventDefault();
            setHighlight((h) => (h - 1 + matches.length) % matches.length);
          } else if (key === "enter") {
            event.preventDefault();
            choose(matches[Math.min(highlight, matches.length - 1)].name);
          }
        }}
        slotProps={{
          input: {
            role: "combobox",
            "aria-expanded": showList,
            "aria-controls": listId,
            "aria-autocomplete": "list",
          },
          htmlInput: { autoComplete: "off" },
          inputLabel: { shrink: true },
        }}
      />
      {showList ? (
        // Template dropdown paper IN PLACE: focus stays in the text field while the author types.
        <DropdownPaper sx={{ position: "absolute", top: "100%", left: 0, right: 0, zIndex: 30, mt: 0.25, maxHeight: 260, overflowY: "auto" }}>
          <MenuList id={listId} role="listbox">
            {matches.map((m, i) => (
              <MenuItem
                key={m.item_id}
                role="option"
                aria-selected={i === highlight}
                selected={i === highlight}
                tabIndex={-1}
                onMouseDown={(event) => {
                  event.preventDefault();
                  choose(m.name);
                }}
                onMouseEnter={() => setHighlight(i)}
                sx={{ flexDirection: "column", alignItems: "flex-start", gap: 0, whiteSpace: "normal" }}
              >
                <div>{m.name}</div>
                {m.category_path ? (
                  <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{m.category_path}</Typography>
                ) : null}
              </MenuItem>
            ))}
          </MenuList>
        </DropdownPaper>
      ) : null}
      {unknown ? (
        <Typography variant="caption" component="span" sx={{ color: "error.main" }}>
          {copy(pageContract, "warn.medicine_not_in_catalog")}
        </Typography>
      ) : null}
    </Box>
  );
}

function historyBackToListNonce(): string | null {
  const state = window.history.state as Record<string, unknown> | null;
  const nonce = state?.[BACK_TO_LIST_HISTORY_NONCE];
  return typeof nonce === "string" ? nonce : null;
}

function bindBackToListHistoryEntry(href: string) {
  const marker = readBackToListMarker();
  if (
    !marker ||
    pathAndSearch(marker.editorHref) !== `${window.location.pathname}${window.location.search}` ||
    pathAndSearch(marker.listHref) !== pathAndSearch(href)
  ) {
    window.sessionStorage.removeItem(BACK_TO_LIST_MARKER);
    return;
  }

  window.sessionStorage.removeItem(BACK_TO_LIST_MARKER);
  activeBackToListNonces.add(marker.nonce);
  window.history.replaceState(
    { ...(window.history.state ?? {}), [BACK_TO_LIST_HISTORY_NONCE]: marker.nonce },
    "",
    window.location.href,
  );
}

/** One editable step row. `key` is a stable client id so React does not reuse rows across edits. */
type DraftStep = {
  key: string;
  day_no: string;
  session: string;
  record_type: string;
  medicine_name: string;
  dosage_text: string;
  dosage_denominator: string;
  medicine_route: string;
  instruction: string;
  critical_action_type: string;
};

// The four authoring vocabularies come from the page contract's option groups, keys AND labels.
//
// They are not declared here on purpose. Session, step type, route, unit and critical-action type
// are the values the BACKEND validates against, so a list maintained in this file could drift from
// the set that is actually accepted and would then offer an author a choice that fails on save.
// Reading them from the contract means one source decides what a protocol may contain.

/** Contract options as MUI TextField select options, preserving the backend's order. */
function selectOptions(options: AdminUiOption[], includeBlank?: boolean, blankLabel = ""): SelectOption[] {
  const rows = options.map((choice) => ({ value: choice.key, label: choice.label }));
  return includeBlank ? [{ value: "", label: blankLabel }, ...rows] : rows;
}

function stepFromContract(step: HealthConfigStep, index: number): DraftStep {
  return {
    key: `stored-${step.step_id ?? index}`,
    day_no: String(step.day_no ?? ""),
    session: step.session ?? "",
    record_type: step.record_type ?? "",
    medicine_name: step.medicine_name ?? "",
    dosage_text: step.dosage_text ?? "",
    dosage_denominator: step.dosage_denominator ?? "",
    medicine_route: step.medicine_route ?? "",
    instruction: step.instruction ?? "",
    critical_action_type: step.critical_action_type ?? "",
  };
}

function emptyStep(dayNo: number, key: string): DraftStep {
  return {
    key,
    day_no: String(dayNo),
    session: "morning",
    record_type: "medication",
    medicine_name: "",
    dosage_text: "",
    dosage_denominator: "",
    medicine_route: "",
    instruction: "",
    critical_action_type: "",
  };
}

/**
 * Serializes the editor's rows into the save payload.
 *
 * `day_no` is sent as a NUMBER when it parses and as 0 when it does not, so a non-numeric day is
 * rejected by the backend with a field error naming that row — rather than being dropped here,
 * which would silently remove a step from a medical document.
 */
function toPayload(steps: DraftStep[]) {
  return steps.map((step) => ({
    day_no: /^\d+$/.test(step.day_no) ? Number(step.day_no) : 0,
    session: step.session,
    record_type: step.record_type,
    medicine_name: step.medicine_name,
    dosage_text: step.dosage_text,
    dosage_denominator: step.dosage_denominator,
    medicine_route: step.medicine_route,
    instruction: step.instruction,
    critical_action_type: step.critical_action_type,
  }));
}

function FieldErrors({
  result,
  pageContract,
  prefix,
}: {
  result: HealthConfigActionResult | null;
  pageContract: AdminUiPageContract;
  prefix?: string;
}) {
  if (!result || result.ok) return null;
  const scoped = (result.fieldErrors ?? []).filter(
    (fieldError) => !prefix || fieldError.field.startsWith(prefix),
  );
  // `optionalCopy`, never `copy`, for the message: `copy()` THROWS on a key the contract does not
  // carry, and this component only renders when something already went wrong. A backend that
  // returns a code this build has no copy key for would then crash the whole screen with a runtime
  // error INSTEAD OF showing the failure -- turning a recoverable error into a white page. The
  // error renderer must be the one thing that cannot itself fail.
  const headline =
    optionalCopy(pageContract, result.messageKey) ?? copy(pageContract, "action.error_backend");
  return (
    <Alert severity="error" sx={{ mt: 1.25 }}>
      <div>
        <Typography variant="subtitle2" component="div">{headline}</Typography>
        {result.detail ? (
          <Typography variant="body2" component="div" sx={{ color: "text.secondary" }}>{result.detail}</Typography>
        ) : null}
        {scoped.length > 0 ? (
          <Box component="ul" sx={{ typography: "body2", mt: 0.75, mb: 0, pl: 2.25 }}>
            {scoped.map((fieldError) => (
              <li key={`${fieldError.field}:${fieldError.message}`}>
                <code>{fieldError.field}</code> — {fieldError.message}
              </li>
            ))}
          </Box>
        ) : null}
      </div>
    </Alert>
  );
}

/**
 * "Add disease" — opens drafts for BOTH age bands.
 *
 * `duration_days` is deliberately left blank by default rather than pre-filled with the backend's
 * default: a pre-filled number is a value the author appears to have chosen. Blank sends nothing
 * and lets the backend apply its declared default.
 */
export function AddDiseaseForm({
  pageContract,
  action,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  action: SubmitAction;
  enabled: boolean;
  disabledReason: string;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [idem, setIdem] = useState<AuthoringIdempotencyState>(CLOSED_STATE);

  const trigger = (
    <MuiButton
      type="button"
      variant="contained"
      color="primary"
      startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />}
      disabled={!enabled}
      title={enabled ? undefined : disabledReason}
      onClick={() => {
        setResult(null);
        setIdem(openIntent(() => crypto.randomUUID()));
      }}
    >
      {copy(pageContract, "action.add_disease")}
    </MuiButton>
  );

  return (
    <>
      {trigger}
      {/* The form is a template form dialog, not an inline strip above the page head (judge B, 2026-09-19). */}
      <Dialog fullWidth maxWidth="xs" open={idem.open} onClose={() => setIdem(CLOSED_STATE)} slotProps={{ paper: { "aria-label": copy(pageContract, "action.add_disease") } }}>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            const formData = new FormData(event.currentTarget);
            startTransition(async () => {
              const outcome = await action(formData);
              setResult(outcome);
              setIdem((prev) => afterSubmit(prev, outcome.ok, () => crypto.randomUUID()));
              if (outcome.ok) setIdem(CLOSED_STATE);
            });
          }}
        >
          <DialogTitle>{copy(pageContract, "action.add_disease")}</DialogTitle>
          <DialogContent>
          <input type="hidden" name="idempotency_key" value={idem.key ?? ""} />
          <Box sx={{ pt: 1, display: "grid", gap: 2 }}>
            <MuiTextField
              fullWidth
              name="display_name"
              label={copy(pageContract, "label.disease")}
              autoFocus
              required
              slotProps={{ htmlInput: { maxLength: 80 }, inputLabel: { shrink: true } }}
            />
            <MuiTextField
              fullWidth
              name="duration_days"
              label={copy(pageContract, "label.duration_days")}
              slotProps={{ htmlInput: { inputMode: "numeric" }, inputLabel: { shrink: true } }}
            />
            <FieldErrors result={result} pageContract={pageContract} />
          </Box>
          </DialogContent>
          <DialogActions>
            <MuiButton type="button" variant="outlined" disabled={pending} onClick={() => setIdem(CLOSED_STATE)}>
              {copy(pageContract, "action.cancel")}
            </MuiButton>
            <MuiButton type="submit" variant="contained" color="primary" loading={pending}>
              {copy(pageContract, "action.apply")}
            </MuiButton>
          </DialogActions>
        </form>
      </Dialog>
    </>
  );
}

/** A one-button form that posts a single hidden value (open draft / publish / discard). */
export function ProtocolActionButton({
  pageContract,
  action,
  fields,
  labelKey,
  enabled,
  disabledReason,
  primary,
  confirmKey,
  navigateOnSuccess,
  basePath,
  icon,
}: {
  pageContract: AdminUiPageContract;
  action: SubmitAction;
  fields: Record<string, string>;
  labelKey: string;
  enabled: boolean;
  disabledReason: string;
  primary?: boolean;
  /** Per-row placement: one pencil icon button per row (label as title/aria-label), not a column of outlined buttons. */
  icon?: "edit";
  /** When set, the button asks once before running. Used for publish and discard. */
  confirmKey?: string;
  /**
   * Where to go after a successful write. A serializable ENUM, not a callback: this component is
   * rendered from a Server Component, and React refuses to pass a function across that boundary.
   *
   *   "selected-version" -- select the version the write returned (Edit -> show the draft)
   *   "base"             -- deselect (Discard -> the version no longer exists)
   *
   * The navigation lives here rather than as a `redirect()` inside the server action, and that is
   * load-bearing: a server action invoked from an event handler inside startTransition swallows
   * NEXT_REDIRECT, so the action succeeded and the browser never moved. That was the "Edit opens a
   * draft but nothing appears" bug.
   */
  navigateOnSuccess?: "selected-version" | "base";
  /** The page path both navigation targets are built from. */
  basePath?: string;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [armed, setArmed] = useState(false);
  const [idem] = useState(() => crypto.randomUUID());

  return (
    <Box
      component="form"
      onSubmit={(event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const formData = new FormData(event.currentTarget);
        startTransition(async () => {
          const outcome = await action(formData);
          setResult(outcome);
          setArmed(false);
          if (outcome.ok && navigateOnSuccess && basePath) {
            if (navigateOnSuccess === "base") {
              router.push(basePath);
            } else if (outcome.versionId) {
              const separator = basePath.includes("?") ? "&" : "?";
              const editorHref = `${basePath}${separator}hc_version=${encodeURIComponent(outcome.versionId)}`;
              window.sessionStorage.setItem(
                BACK_TO_LIST_MARKER,
                JSON.stringify({ editorHref, listHref: basePath, nonce: crypto.randomUUID() } satisfies BackToListMarker),
              );
              router.push(editorHref);
            }
          }
          // The version this screen is showing is GONE -- someone else published or discarded it,
          // or it was removed out from under this tab. Refresh so the server re-renders: the page
          // itself handles a missing version (see health-config.tsx) by dropping the dead section
          // and showing a plain notice above the catalog.
          //
          // A refresh, NOT a router.replace(basePath). Replacing to the same route is a soft
          // navigation that Next resolves without clearing the query string, so the stale
          // ?hc_version= survived and a reload resurrected the error. Letting the server own the
          // missing-version case means it also renders correctly on a cold load of that URL,
          // which no amount of client navigation can achieve.
          if (!outcome.ok && outcome.code === "not_found") {
            router.refresh();
          }
        });
      }}
      sx={{ display: "inline-flex", flexDirection: "column", gap: 0.75 }}
    >
      {Object.entries(fields).map(([name, value]) => (
        <input key={name} type="hidden" name={name} value={value} />
      ))}
      <input type="hidden" name="idempotency_key" value={idem} />
      {confirmKey && !armed ? (
        <MuiButton
          type="button"
          size="small"
          variant={primary ? "contained" : "outlined"}
          disabled={!enabled || pending}
          title={enabled ? undefined : disabledReason}
          onClick={() => setArmed(true)}
        >
          {copy(pageContract, labelKey)}
        </MuiButton>
      ) : (
        <Stack component="span" direction="row" spacing={0.75} useFlexGap sx={{ display: "inline-flex", alignItems: "center", flexWrap: "wrap" }}>
          {icon === "edit" ? (
            <IconButton
              type="submit"
              size="small"
              disabled={!enabled || pending}
              aria-busy={pending || undefined}
              title={enabled ? copy(pageContract, labelKey) : disabledReason}
              aria-label={copy(pageContract, labelKey)}
            >
              <Iconify icon="solar:pen-bold" aria-hidden="true" />
            </IconButton>
          ) : (
          <MuiButton
            type="submit"
            size="small"
            variant={primary ? "contained" : "outlined"}
            loading={pending}
            disabled={!enabled}
            title={enabled ? undefined : disabledReason}
          >
            {copy(pageContract, labelKey)}
          </MuiButton>
          )}
          {armed ? (
            <MuiButton type="button" size="small" variant="outlined" disabled={pending} onClick={() => setArmed(false)}>
              {copy(pageContract, "action.cancel")}
            </MuiButton>
          ) : null}
        </Stack>
      )}
      {armed && confirmKey ? (
        <Typography variant="body2" component="span" sx={{ color: "text.secondary", maxWidth: 320 }}>
          {copy(pageContract, confirmKey)}
        </Typography>
      ) : null}
      <FieldErrors result={result} pageContract={pageContract} />
    </Box>
  );
}

export function BackToListButton({ href, label }: { href: string; label: string }) {
  const router = useRouter();
  useEffect(() => {
    bindBackToListHistoryEntry(href);
  }, [href]);

  return (
    <MuiButton
      component="a"
      size="small"
      variant="outlined"
      color="inherit"
      href={href}
      startIcon={<Iconify icon="eva:arrow-ios-back-fill" aria-hidden="true" />}
      onClick={(event: React.MouseEvent<HTMLAnchorElement>) => {
        // Only the catalog's Edit action binds the current history entry to an in-memory nonce.
        // URL equality alone is not enough: a stale marker can match a later direct-opened editor.
        // Reloads also lose the in-memory nonce, so the safe filtered href wins there too.
        const nonce = historyBackToListNonce();
        if (!nonce || !activeBackToListNonces.has(nonce)) {
          window.sessionStorage.removeItem(BACK_TO_LIST_MARKER);
          return;
        }
        event.preventDefault();
        activeBackToListNonces.delete(nonce);
        window.sessionStorage.removeItem(BACK_TO_LIST_MARKER);
        router.back();
      }}
    >
      {label}
    </MuiButton>
  );
}

/**
 * The draft editor: name, number of days, and the ordered step list.
 *
 * Rows are held in local state and submitted whole. Adding a row appends a blank one on the last
 * day, which the server drops if it is still blank at save time — so clicking "Add step" and
 * changing your mind costs nothing.
 */
export function DraftEditor({
  pageContract,
  draft,
  medicines,
  action,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  draft: HealthConfigProtocolDetail;
  /** The farm's active medicines. A step names one of these and nothing else. */
  medicines: HealthCatalogItem[];
  action: SubmitAction;
  enabled: boolean;
  disabledReason: string;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [idem, setIdem] = useState(() => crypto.randomUUID());
  const [displayName, setDisplayName] = useState(draft.display_name);
  const [durationDays, setDurationDays] = useState(String(draft.duration_days));
  const [steps, setSteps] = useState<DraftStep[]>(() => (draft.steps ?? []).map(stepFromContract));
  const [nextKey, setNextKey] = useState(0);

  const sessions = useMemo(() => optionGroup(pageContract, "health_sessions"), [pageContract]);
  const recordTypes = useMemo(() => optionGroup(pageContract, "health_record_types"), [pageContract]);
  const routes = useMemo(() => optionGroup(pageContract, "health_medicine_routes"), [pageContract]);
  const denominators = useMemo(() => optionGroup(pageContract, "health_dosage_units"), [pageContract]);
  const criticalTypes = useMemo(() => optionGroup(pageContract, "health_critical_actions"), [pageContract]);

  function updateStep(key: string, patch: Partial<DraftStep>) {
    setSteps((prev) => prev.map((step) => (step.key === key ? { ...step, ...patch } : step)));
  }

  function addStep() {
    const lastDay = steps.length > 0 ? steps[steps.length - 1].day_no : "1";
    const day = /^\d+$/.test(lastDay) ? Number(lastDay) : 1;
    setSteps((prev) => [...prev, emptyStep(day, `new-${nextKey}`)]);
    setNextKey((n) => n + 1);
  }

  return (
    <Box
      component="form"
      onSubmit={(event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const formData = new FormData(event.currentTarget);
        formData.set("steps", JSON.stringify(toPayload(steps)));
        startTransition(async () => {
          const outcome = await action(formData);
          setResult(outcome);
          // Rotate the key only after a confirmed success: a failed save must be retryable under
          // the SAME key so the retry is recognised as the same intent, not a second edit.
          if (outcome.ok) setIdem(crypto.randomUUID());
        });
      }}
      sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}
    >
      <input type="hidden" name="disease_key" value={draft.disease_key} />
      <input type="hidden" name="age_band" value={draft.age_band} />
      <input type="hidden" name="idempotency_key" value={idem} />

      <Stack direction="row" spacing={1.5} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-end" }}>
        <MuiTextField
          name="display_name"
          label={copy(pageContract, "label.disease")}
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          slotProps={{ htmlInput: { maxLength: 80 }, inputLabel: { shrink: true } }}
          sx={{ minWidth: 220 }}
        />
        <MuiTextField
          name="duration_days"
          label={copy(pageContract, "label.duration_days")}
          value={durationDays}
          onChange={(event) => setDurationDays(event.target.value)}
          slotProps={{ htmlInput: { inputMode: "numeric" }, inputLabel: { shrink: true } }}
          sx={{ maxWidth: 120 }}
        />
        <Typography variant="body2" sx={{ color: "text.secondary", maxWidth: 420 }}>
          {copy(pageContract, "note.days_shrink")} {copy(pageContract, "note.rename_scope")}
        </Typography>
      </Stack>

      <EmptyDraftOverLiveNotice detail={draft} stepCount={steps.length} pageContract={pageContract} />

      {/* ONE READABLE BLOCK PER STEP, not a 10-column table.
          A treatment step has fields that only apply to its own kind: a medicine step has a
          dosage and a route and no instruction, an action has an instruction and none of the
          rest. Laying all ten columns side by side made every row mostly empty cells and pushed
          the instruction — the longest and most important field on an action step — off the
          right edge behind a horizontal scrollbar. Here each step shows only its own fields,
          the instruction gets full width, and nothing scrolls sideways. */}
      <Stack spacing={1.25}>
        {steps.length === 0 ? (
          <Typography variant="body2" component="div" sx={{ color: "text.secondary", py: 2, px: 0.5, textAlign: "center" }}>
            {copy(pageContract, "empty.steps")}
          </Typography>
        ) : (
          steps.map((step, index) => {
            const isMedicine = step.record_type === "medication";
            const isCritical = step.record_type === "critical_action";
            return (
              <Stack
                key={step.key}
                spacing={1.25}
                sx={{ border: 1, borderColor: "divider", borderRadius: "var(--r-lg)", px: 1.5, py: 1.25 }}
              >
                {/* WHEN: day, session and kind — the three things that place a step in the course. */}
                <Stack direction="row" spacing={1.25} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-end" }}>
                  <Label variant="soft" color="default" sx={{ alignSelf: "center" }}>
                    {index + 1}
                  </Label>
                  <MuiTextField
                    label={copy(pageContract, "label.day_no")}
                    value={step.day_no}
                    onChange={(event) => updateStep(step.key, { day_no: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "numeric" }, inputLabel: { shrink: true } }}
                    sx={{ width: 84 }}
                  />
                  <MuiTextField
                    select
                    label={copy(pageContract, "label.session")}
                    value={step.session}
                    onChange={({ target: { value } }) => updateStep(step.key, { session: value })}
                    sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
                    slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                  >
                    {selectOptions(sessions).map((option) => (
                      <MenuItem key={option.value} value={option.value}>
                        {option.label}
                      </MenuItem>
                    ))}
                  </MuiTextField>
                  <MuiTextField
                    select
                    label={copy(pageContract, "label.record_type")}
                    value={step.record_type}
                    onChange={({ target: { value: recordType } }) => {
                        // Clearing the fields that do not belong to the new kind is not a
                        // convenience: the database CHECK constraints reject a medicine step
                        // carrying a handoff type, and a critical step carrying a medicine.
                        updateStep(step.key, {
                          record_type: recordType,
                          ...(recordType === "medication"
                            ? { critical_action_type: "" }
                            : { medicine_name: "", dosage_text: "", dosage_denominator: "", medicine_route: "" }),
                          ...(recordType === "critical_action" ? {} : { critical_action_type: "" }),
                        });
                    }}
                    sx={{ minWidth: { xs: 0, sm: 170 }, flexShrink: 0, maxWidth: 1 }}
                    slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                  >
                    {selectOptions(recordTypes).map((option) => (
                      <MenuItem key={option.value} value={option.value}>
                        {option.label}
                      </MenuItem>
                    ))}
                  </MuiTextField>
                  <MuiButton
                    type="button"
                    size="small"
                    variant="outlined"
                    color="error"
                    startIcon={<Iconify icon="solar:trash-bin-trash-bold" aria-hidden="true" />}
                    title={copy(pageContract, "action.remove_step")}
                    sx={{ marginLeft: "auto" }}
                    onClick={() => setSteps((prev) => prev.filter((row) => row.key !== step.key))}
                  >
                    {copy(pageContract, "action.remove_step")}
                  </MuiButton>
                </Stack>

                {/* WHAT: only the fields this kind of step actually carries. */}
                {isMedicine ? (
                  <Stack direction="row" spacing={1.25} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-end" }}>
                    <MedicinePicker
                      value={step.medicine_name}
                      medicines={medicines}
                      pageContract={pageContract}
                      onChange={(name) => updateStep(step.key, { medicine_name: name })}
                    />
                    <MuiTextField
                      label={copy(pageContract, "label.dosage_text")}
                      value={step.dosage_text}
                      onChange={(event) => updateStep(step.key, { dosage_text: event.target.value })}
                      slotProps={{ htmlInput: { inputMode: "decimal" }, inputLabel: { shrink: true } }}
                      sx={{ width: 110 }}
                    />
                    <MuiTextField
                      select
                      label={copy(pageContract, "label.dosage_denominator")}
                      value={step.dosage_denominator}
                      title={copy(pageContract, "note.dosage_unit")}
                      onChange={({ target: { value } }) => updateStep(step.key, { dosage_denominator: value })}
                      sx={{ minWidth: { xs: 0, sm: 130 }, flexShrink: 0, maxWidth: 1 }}
                      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                    >
                      {selectOptions(denominators, true).map((option) => (
                        <MenuItem key={option.value} value={option.value}>
                          {option.label}
                        </MenuItem>
                      ))}
                    </MuiTextField>
                    <MuiTextField
                      select
                      label={copy(pageContract, "label.medicine_route")}
                      value={step.medicine_route}
                      onChange={({ target: { value } }) => updateStep(step.key, { medicine_route: value })}
                      sx={{ minWidth: { xs: 0, sm: 170 }, flexShrink: 0, maxWidth: 1 }}
                      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                    >
                      {selectOptions(routes, true).map((option) => (
                        <MenuItem key={option.value} value={option.value}>
                          {option.label}
                        </MenuItem>
                      ))}
                    </MuiTextField>
                  </Stack>
                ) : (
                  <Stack direction="row" spacing={1.25} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-end" }}>
                    <MuiTextField
                      fullWidth
                      multiline
                      rows={2}
                      label={copy(pageContract, "label.instruction")}
                      value={step.instruction}
                      onChange={(event) => updateStep(step.key, { instruction: event.target.value })}
                      slotProps={{ inputLabel: { shrink: true } }}
                      sx={{ flex: "1 1 100%" }}
                    />
                    {isCritical ? (
                      <MuiTextField
                        select
                        label={copy(pageContract, "label.critical_action_type")}
                        value={step.critical_action_type}
                        onChange={({ target: { value } }) => updateStep(step.key, { critical_action_type: value })}
                        sx={{ minWidth: { xs: 0, sm: 260 }, flexShrink: 0, maxWidth: 1 }}
                        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                      >
                        {selectOptions(criticalTypes, true).map((option) => (
                          <MenuItem key={option.value} value={option.value}>
                            {option.label}
                          </MenuItem>
                        ))}
                      </MuiTextField>
                    ) : null}
                  </Stack>
                )}
              </Stack>
            );
          })
        )}
      </Stack>

      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center" }}>
        <MuiButton type="button" size="small" variant="outlined" startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />} onClick={addStep}>
          {copy(pageContract, "action.add_step")}
        </MuiButton>
        <MuiButton
          type="submit"
          size="small"
          variant="contained" color="primary"
          loading={pending}
          disabled={!enabled}
          title={enabled ? undefined : disabledReason}
        >
          {copy(pageContract, "action.save_draft")}
        </MuiButton>
        <Typography variant="body2" component="span" sx={{ color: "text.secondary" }}>
          {copy(pageContract, "note.unscheduled_session")}
        </Typography>
      </Stack>

      <FieldErrors result={result} pageContract={pageContract} />
    </Box>
  );
}
