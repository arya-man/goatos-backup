"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useCallback, useMemo, useState, useTransition } from "react";

import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Stack from "@mui/material/Stack";
import { Scrollbar } from "@/components/minimal/scrollbar";
import Button from "@mui/material/Button";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";

import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AccessModuleWrite, PersonAccess } from "@/lib/api/server";
import { designationDefaultsAction, savePersonAccessAction } from "./access-actions";
import Alert from "@mui/material/Alert";

/**
 * The per-person access editor.
 *
 * One question per row: what may this person do with this module, on the web
 * console and on the phone. Every visible word — module names, capability names,
 * the blurbs, the warning — comes from `access`, which the backend composed. The
 * raw vocabulary behind these chips is `aas_health` and `oversee`, and neither is
 * a word to put in front of someone deciding what a colleague may do.
 *
 * Local state only until Save. Toggling a chip must not navigate or re-render the
 * page beneath (the local-overlay contract), so every control here is a button.
 *
 * This renders the BODY of the launcher's MUI Dialog (template custom-dialog layout:
 * DialogTitle / DialogContent / DialogActions). The Dialog owns the portal, the focus trap,
 * Escape / scrim close and focus return to the Access button.
 */

type Draft = {
  designationCode: string;
  scopeMode: "tenant" | "parks";
  parkIDs: string[];
  /** The one park per-park work is assigned in; asked only when more than one park is ticked. */
  homeParkID: string;
  /** The parks whose pens this person visits the day after care work (a subset of parkIDs). */
  penVisitParkIDs: string[];
  /** module_key -> surface -> capability levels, plus the web page ticks */
  modules: Record<string, { web: string[]; mobile: string[]; pages: string[] }>;
};

function draftFrom(access: PersonAccess): Draft {
  const modules: Draft["modules"] = {};
  for (const row of access.modules) {
    modules[row.module_key] = {
      web: [...row.granted_web],
      mobile: [...row.granted_mobile],
      pages: [...row.granted_pages_web],
    };
  }
  return {
    designationCode: access.designation_code ?? "",
    scopeMode: access.scope_mode === "tenant" ? "tenant" : "parks",
    parkIDs: [...access.park_ids],
    homeParkID: access.home_park_id ?? "",
    penVisitParkIDs: [...(access.pen_visit_park_ids ?? [])],
    modules,
  };
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

export function PersonAccessModal({
  access,
  pageContract,
  onClose,
}: {
  access: PersonAccess;
  pageContract: AdminUiPageContract;
  onClose: () => void;
}) {
  // Capability-gated, never a role check in the component (AGENTS.md, the 2026-08-12 STG
  // incident). The PUT route requires the same permission, so this is the honest label
  // rather than the lock.
  const mayEdit = controlEnabled(pageContract, "edit_access", false);
  const cannotEditReason = control(pageContract, "edit_access").disabled_reason;
  const t = (key: string) => copy(pageContract, key);
  const [draft, setDraft] = useState<Draft>(() => draftFrom(access));
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();

  // No re-seed effect: the launcher mounts this only once it has the record and
  // discards the record on close, so the useState initialiser above is the only seed
  // that can run. A successful save closes the editor, and a failed one deliberately
  // keeps what the admin typed so they can correct one tick rather than start again.

  const setPage = useCallback((moduleKey: string, pageKey: string) => {
    setDraft((current) => {
      const row = current.modules[moduleKey] ?? { web: [], mobile: [], pages: [] };
      return {
        ...current,
        modules: { ...current.modules, [moduleKey]: { ...row, pages: toggle(row.pages, pageKey) } },
      };
    });
  }, []);

  const setCapability = useCallback((moduleKey: string, surface: "web" | "mobile", level: string) => {
    setDraft((current) => {
      const row = current.modules[moduleKey] ?? { web: [], mobile: [], pages: [] };
      const next = { ...row, [surface]: toggle(row[surface], level) };
      // Granting a module for the first time opens it on every screen it has. That is
      // what the backend does with an empty stored list, and the alternative -- a
      // module ticked with no screens -- is refused on save rather than silently
      // widened, so the editor must never leave it in that state.
      if (surface === "web" && next.web.length > 0 && next.pages.length === 0) {
        const catalog = access.modules.find((m) => m.module_key === moduleKey);
        next.pages = (catalog?.pages ?? []).map((page) => page.page_key);
      }
      return { ...current, modules: { ...current.modules, [moduleKey]: next } };
    });
  }, [access.modules]);

  const applyDesignation = useCallback(
    (code: string) => {
      setDraft((current) => ({ ...current, designationCode: code }));
      if (!code) return;
      // Pre-fill from the designation's defaults. It REPLACES the module ticks
      // rather than merging: "start this person from Feed Director" is the whole
      // point, and merging would leave whatever was there before silently on.
      startTransition(async () => {
        const result = await designationDefaultsAction(code);
        if (!result.ok) {
          setError(t("access.error.defaults"));
          return;
        }
        setDraft((current) => {
          // Every rendered module is reset first, so applying a designation is a
          // clean start rather than a merge over whatever happened to be ticked.
          const modules: Draft["modules"] = {};
          for (const row of access.modules) modules[row.module_key] = { web: [], mobile: [], pages: [] };
          for (const row of result.modules) {
            modules[row.module_key] = {
              web: [...row.web],
              mobile: [...row.mobile],
              pages: [...(row.pages ?? [])],
            };
          }
          return { ...current, modules };
        });
      });
    },
    [access.modules],
  );

  const grantedCount = useMemo(
    () =>
      Object.values(draft.modules).filter((row) => row.web.length > 0 || row.mobile.length > 0).length,
    [draft.modules],
  );

  const capabilityLabel = useCallback(
    (level: string) => access.capabilities.find((c) => c.level === level),
    [access.capabilities],
  );

  const submit = useCallback(() => {
    setError("");
    const modules: AccessModuleWrite[] = access.modules.map((row) => ({
      module_key: row.module_key,
      web: draft.modules[row.module_key]?.web ?? [],
      mobile: draft.modules[row.module_key]?.mobile ?? [],
      pages: draft.modules[row.module_key]?.pages ?? [],
    }));
    startTransition(async () => {
      const result = await savePersonAccessAction({
        personId: access.person_id,
        body: {
          designation_code: draft.designationCode,
          scope_mode: draft.scopeMode,
          park_ids: draft.scopeMode === "parks" ? draft.parkIDs : [],
          // One ticked park IS the home park; the select only exists past one. The
          // backend validates the same rule, so this is the honest value, not the lock.
          home_park_id:
            draft.scopeMode === "parks" && draft.parkIDs.length === 1 ? draft.parkIDs[0] : draft.homeParkID,
          // Sent whole: an unticked park is absent. Narrowed to covered parks so the
          // backend's rule reads the same list the screen shows.
          pen_visit_park_ids: draft.penVisitParkIDs.filter(
            (id) => draft.scopeMode === "tenant" || draft.parkIDs.includes(id),
          ),
          modules,
          row_version: access.row_version,
        },
      });
      if (result.ok) {
        onClose();
        return;
      }
      // Backend-composed copy, rendered verbatim: it names WHICH tick was refused.
      setError(result.message || t("access.error.save"));
    });
  }, [access.modules, access.person_id, access.row_version, draft, onClose]);

  return (
    <>
        <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5, pr: 1.5 }}>
          <Iconify icon="solar:shield-check-bold" width={22} sx={{ color: "primary.main", flexShrink: 0 }} />
          <Box sx={{ minWidth: 0, flexGrow: 1 }}>
            <Typography variant="h6" component="h2">
              {t("access.title")} · {access.display_name}
            </Typography>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {access.email ? `${access.email} · ` : ""}
              {grantedCount === 0
                ? t("access.summary.none")
                : `${grantedCount} ${t("access.summary.count").replace("{total}", String(access.modules.length))}`}
            </Typography>
          </Box>
          <IconButton onClick={onClose} aria-label={t("action.close")}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>

        <DialogContent dividers sx={{ display: "flex", flexDirection: "column", gap: 2, pt: 1 }}>
          {error ? (
            <Alert severity="error" role="alert">
              {error}
            </Alert>
          ) : null}

          {access.warnings.map((warning) => (
            <Alert key={warning.module_key} severity="warning">
              <b>{t("access.warning.title")}</b> {warning.message}
            </Alert>
          ))}

          <Stack spacing={2.5}>
            <TextField
              select
              label={t("access.designation")}
              value={draft.designationCode}
              disabled={pending || !mayEdit}
              onChange={(event) => applyDesignation(event.target.value)}
              sx={{ flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              <MenuItem value="">{t("access.designation.none")}</MenuItem>
              {access.designations.map((designation) => (
                <MenuItem key={designation.code} value={designation.code}>
                  {designation.label}
                </MenuItem>
              ))}
            </TextField>

            <Box>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>{t("access.scope")}</Typography>
              <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", rowGap: 1 }}>
                <ToggleChip
                  label={t("access.scope.tenant")}
                  on={draft.scopeMode === "tenant"}
                  onClick={() => setDraft((c) => ({ ...c, scopeMode: "tenant" }))}
                />
                {access.parks.map((park) => (
                  <ToggleChip
                    key={park.park_id}
                    label={park.label}
                    on={draft.scopeMode === "parks" && draft.parkIDs.includes(park.park_id)}
                    onClick={() =>
                      setDraft((c) => {
                        const parkIDs = toggle(c.scopeMode === "parks" ? c.parkIDs : [], park.park_id);
                        // An unticked park cannot stay the home park, nor a pen-visit park.
                        const homeParkID = parkIDs.includes(c.homeParkID) ? c.homeParkID : "";
                        const penVisitParkIDs = c.penVisitParkIDs.filter((id) => parkIDs.includes(id));
                        return { ...c, scopeMode: "parks", parkIDs, homeParkID, penVisitParkIDs };
                      })
                    }
                  />
                ))}
              </Stack>
            </Box>

            {draft.scopeMode === "parks" && draft.parkIDs.length > 1 ? (
              <TextField
                select
                label={t("access.home_park")}
                value={draft.homeParkID}
                disabled={!mayEdit || pending}
                onChange={({ target: { value: homeParkID } }) => setDraft((c) => ({ ...c, homeParkID }))}
                helperText={t("access.home_park.hint")}
                sx={{ flexShrink: 0, maxWidth: 1 }}
                slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
              >
                <MenuItem value="">{t("access.home_park.none")}</MenuItem>
                {access.parks
                    .filter((park) => draft.parkIDs.includes(park.park_id)).map((park) => (
                  <MenuItem key={park.park_id} value={park.park_id}>
                    {park.label}
                  </MenuItem>
                ))}
              </TextField>
            ) : null}

            {/* Pen visits (maintainer decision 2026-09-12): which parks' pens this person walks
                the day after vaccination or care work. Per-park, one or more people, any of
                whom may record; the label and blurb are backend copy. Only parks the person
                covers are offered, so a visit is never owed to someone whose scope cannot
                reach it. */}
            <Box>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>{access.pen_visit_label}</Typography>
              <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", rowGap: 1 }} data-testid="pa-pen-visit-parks">
                {access.parks
                  .filter((park) => draft.scopeMode === "tenant" || draft.parkIDs.includes(park.park_id))
                  .map((park) => (
                    <ToggleChip
                      key={park.park_id}
                      label={park.label}
                      on={draft.penVisitParkIDs.includes(park.park_id)}
                      disabled={!mayEdit || pending}
                      onClick={() => setDraft((c) => ({ ...c, penVisitParkIDs: toggle(c.penVisitParkIDs, park.park_id) }))}
                    />
                  ))}
              </Stack>
              <Typography variant="caption" component="div" sx={{ color: "text.secondary", mt: 1 }}>{access.pen_visit_blurb}</Typography>
            </Box>
          </Stack>

          <Scrollbar>
            <Table size="small" sx={{ minWidth: 560 }}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{t("access.column.module")}</TableCell>
                  <TableCell component="th">{t("access.column.web")}</TableCell>
                  <TableCell component="th">{t("access.column.mobile")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {access.modules.map((row) => {
                  const held = draft.modules[row.module_key] ?? { web: [], mobile: [], pages: [] };
                  const hasAny = held.web.length > 0 || held.mobile.length > 0;
                  return (
                    <TableRow key={row.module_key} selected={hasAny}>
                      <TableCell sx={{ minWidth: 200 }}>
                        <Typography variant="subtitle2">{row.label}</Typography>
                        <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{row.blurb}</Typography>
                      </TableCell>
                      {(["web", "mobile"] as const).map((surface) => {
                        const offered = surface === "web" ? row.offered_web : row.offered_mobile;
                        if (offered.length === 0) {
                          return (
                            <TableCell key={surface}>
                              <Typography variant="caption" sx={{ color: "text.disabled" }}>
                                {t(surface === "web" ? "access.unavailable.web" : "access.unavailable.mobile")}
                              </Typography>
                            </TableCell>
                          );
                        }
                        // Page ticks belong to the WEB cell alone and only once the
                        // module is granted: which screens someone keeps is a question
                        // that only exists after they have the module at all.
                        const showPages =
                          surface === "web" && row.pages.length > 0 && held.web.length > 0;
                        return (
                          <TableCell key={surface}>
                            <Stack direction="row" spacing={0.75} sx={{ flexWrap: "wrap", rowGap: 0.75 }}>
                              {offered.map((level) => {
                                const copy = capabilityLabel(level);
                                return (
                                  <ToggleChip
                                    key={level}
                                    label={copy?.label ?? level}
                                    title={copy?.blurb}
                                    size="small"
                                    on={held[surface].includes(level)}
                                    disabled={pending || !mayEdit}
                                    onClick={() => setCapability(row.module_key, surface, level)}
                                  />
                                );
                              })}
                            </Stack>
                            {showPages ? (
                              <Stack direction="row" spacing={0.75} sx={{ flexWrap: "wrap", rowGap: 0.75, mt: 1 }} role="group" aria-label={t("access.pages.label")}>
                                {row.pages.map((page) => (
                                  <ToggleChip
                                    key={page.page_key}
                                    label={page.label}
                                    size="small"
                                    tone="info"
                                    on={held.pages.includes(page.page_key)}
                                    disabled={pending || !mayEdit}
                                    onClick={() => setPage(row.module_key, page.page_key)}
                                  />
                                ))}
                              </Stack>
                            ) : null}
                          </TableCell>
                        );
                      })}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
        </DialogContent>

        <DialogActions>
          <Button variant="outlined" color="inherit" onClick={onClose} disabled={pending}>
            {t("action.cancel")}
          </Button>
          {/* A disabled button swallows its title, so the reason rides on a wrapping span. */}
          <Box component="span" title={mayEdit ? undefined : cannotEditReason}>
            <Button
              variant="contained" color="primary"
              onClick={submit}
              disabled={pending || !mayEdit}
              aria-disabled={!mayEdit}
            >
              {pending ? t("access.action.saving") : t("access.action.save")}
            </Button>
          </Box>
        </DialogActions>
    </>
  );
}

/** A pressable template Chip: soft primary when on, outlined when off (aria-pressed carries the state). */
function ToggleChip({
  label,
  on,
  onClick,
  disabled,
  title,
  size = "medium",
  tone = "primary",
}: {
  label: string;
  on: boolean;
  onClick: () => void;
  disabled?: boolean;
  title?: string;
  size?: "small" | "medium";
  tone?: "primary" | "info";
}) {
  return (
    <Chip
      label={label}
      title={title}
      size={size}
      clickable
      disabled={disabled}
      color={on ? tone : "default"}
      variant={on ? "soft" : "outlined"}
      aria-pressed={on}
      onClick={onClick}
      icon={on ? <Iconify icon="eva:checkmark-fill" width={16} /> : undefined}
    />
  );
}
