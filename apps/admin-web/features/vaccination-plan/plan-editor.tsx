"use client";

/**
 * Vaccination plan editor — screen 2 of the console.
 *
 * Template product new/edit anatomy: selector column (md 4) beside the form column (md 8), Card +
 * CardHeader sections, MUI Switch / ToggleButtonGroup / Chip / Dialog, a pinned action Paper.
 *
 * Everything on this screen edits a DRAFT. Nothing here reaches the field until
 * Publish, which is why the action bar is pinned to the bottom rather than
 * sitting above a long scroll: the decision to publish is made after reading,
 * not before.
 */

import Link from "@/components/no-prefetch-link";
import { PageRoot } from "@/components/app/page-root";
import { useRouter } from "next/navigation";
import MuiButton from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import MuiTextField from "@mui/material/TextField";
import Switch from "@mui/material/Switch";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import FormControlLabel from "@mui/material/FormControlLabel";
import Grid from "@mui/material/Grid";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Paper from "@mui/material/Paper";
import Radio from "@mui/material/Radio";
import Stack from "@mui/material/Stack";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";

import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import { PageHeader } from "@/components/app/page-header";
import MenuItem from "@mui/material/MenuItem";
import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import type React from "react";

import { DurationField, formatDays } from "./duration-field";
import type { AnchorConfig, EditorPlan, EditorVaccine, NewVaccineInput, ProcurementPurpose } from "./editor-model";
import { newVaccineToEditor } from "./editor-model";
import { publishPlan, saveDraftPlan } from "./plan-actions";
import { VaccinationAnchorPanel } from "./anchor-panel";
import { humanDays } from "./plan-model";
import type { ScheduleRule } from "./plan-model";
import Alert from "@mui/material/Alert";

type Props = {
  protocolId: string;
  draftVersionId: string;
  draftLabel: string;
  liveLabel: string | null;
  liveSince: string | null;
  scopeType: string;
  originalRuleDsl: unknown;
  proofPolicy: unknown;
  initialPlan: EditorPlan;
  impact: ImpactSummary | null;
  canPublish: boolean;
  // The backend's own words when it has them; the fallback below is this screen's.
  cannotPublishReason: string | null;
};

export type ImpactSummary = {
  eligibleAnimals: number;
  affectedSheds: number;
  estimatedDays: number;
  dailyCap: number;
  capacityStatus?: string;
};

export function VaccinationPlanEditor(props: Props) {
  const router = useRouter();
  const [plan, setPlan] = useState<EditorPlan>(props.initialPlan);
  // The id of the draft that is actually live RIGHT NOW.
  //
  // Saving replaces the draft: a new version carries the edits and the old row is
  // discarded. props.draftVersionId only catches up when the server components finish
  // re-rendering, and the buttons re-enable before that -- so a Publish clicked in that
  // window went to the id the save had just discarded and told the user their draft could
  // not be read, when it was perfectly fine. A ref is updated synchronously, so it is
  // right the instant the save returns.
  const liveVersionId = useRef(props.draftVersionId);
  // Likewise the document the edits are written back onto: a second save in that window
  // re-derived from the pre-save original and dropped what the first save wrote.
  const liveRuleDsl = useRef(props.originalRuleDsl);
  useEffect(() => {
    liveVersionId.current = props.draftVersionId;
    liveRuleDsl.current = props.originalRuleDsl;
  }, [props.draftVersionId, props.originalRuleDsl]);

  // What "unchanged" means, which is not the same as what the page was first given.
  //
  // The server assigns real dose codes to doses the editor invented, so after a save the
  // saved plan differs from client state by those codes alone. Comparing against the page
  // props therefore never settled: the "Draft saved." note never appeared and Save stayed
  // lit, inviting the user to save the same thing forever.
  // Held as "what was last saved", defaulting to what the page was given. Kept as state
  // rather than synced from props in an effect: setting state inside an effect re-renders
  // the whole editor a second time on every prop change, and the value is only ever
  // replaced at two moments this component already knows about -- a save, and a fresh page.
  const [savedPlan, setSavedPlan] = useState<EditorPlan | null>(null);
  const baseline = savedPlan ?? props.initialPlan;
  const setBaseline = setSavedPlan;
  const [selected, setSelected] = useState(props.initialPlan.vaccines[0]?.code ?? "");
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [addingVaccine, setAddingVaccine] = useState(false);
  const [leaveConfirm, setLeaveConfirm] = useState(false);

  const dirty = useMemo(
    () => JSON.stringify(plan) !== JSON.stringify(baseline),
    [plan, baseline],
  );
  const selectedSetting = selected === "__procurement" || selected === "__safety" ? selected : null;
  const current = plan.vaccines.find((v) => v.code === selected) ?? plan.vaccines[0];
  const currentAnchorRows = useMemo(() => ruleRowsForVaccine(current), [current]);
  const onCount = plan.vaccines.filter((v) => v.on).length;

  // A vaccine switched on with no doses is not in the plan: "off" IS an empty
  // schedule, so saving one round-trips it straight back to off and the switch
  // silently flips back. Rather than let that happen quietly, saving is blocked
  // until the vaccine has a dose or is switched off again.
  const emptyOn = plan.vaccines.filter((v) => v.on && v.kidDoses.length === 0 && v.driveDoses.length === 0);
  const blockedReason =
    emptyOn.length > 0
      ? `${emptyOn.map((v) => v.name).join(", ")} ${emptyOn.length === 1 ? "is" : "are"} switched on but ${
          emptyOn.length === 1 ? "has" : "have"
        } no doses. Add a dose, or switch ${emptyOn.length === 1 ? "it" : "them"} off.`
      : null;

  // Unsaved edits die silently on a stray click of the backlink or a browser back.
  // Discarding a draft already demands confirmation; losing an hour of authoring to a
  // misclick should not be quieter than that.
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  function updateVaccine(code: string, change: (v: EditorVaccine) => EditorVaccine) {
    setSaved(false);
    setPlan((p) => ({ ...p, vaccines: p.vaccines.map((v) => (v.code === code ? change(v) : v)) }));
  }

  function updateAnchor(doseCode: string, anchor: AnchorConfig | null) {
    if (!current) return;
    updateVaccine(current.code, (v) => {
      const anchors = { ...(v.anchors ?? {}) };
      if (anchor) anchors[doseCode] = anchor;
      else delete anchors[doseCode];
      return { ...v, anchors };
    });
  }

  /**
   * Validated the same way the mock validates it: name and short code required,
   * and neither may already belong to a vaccine already in this draft. Returns
   * an error string for the panel to show inline, or null on success -- the
   * panel stays open on an error, exactly like every other save-time validation
   * in this editor.
   */
  function onAddVaccine(input: NewVaccineInput): string | null {
    const name = input.name.trim();
    const code = input.code.trim();
    if (!name || !code) return "Name and short code are required.";
    const lowerCode = code.toLowerCase();
    const codeClash = plan.vaccines.find((v) => v.code.toLowerCase() === lowerCode);
    if (codeClash) return `Short code "${code}" is already used by ${codeClash.name}.`;
    const nameClash = plan.vaccines.find((v) => v.name.toLowerCase() === name.toLowerCase());
    if (nameClash) return `"${name}" is already in this plan.`;
    const vaccine = newVaccineToEditor({ ...input, name, code });
    setSaved(false);
    setPlan((p) => ({ ...p, vaccines: [...p.vaccines, vaccine] }));
    setSelected(vaccine.code);
    setAddingVaccine(false);
    return null;
  }

  function onSave() {
    setError(null);
    startTransition(async () => {
      const result = await saveDraftPlan(liveVersionId.current, plan, liveRuleDsl.current);
      if (!result.ok) {
        setError(result.error);
        return;
      }
      setSaved(true);
      setBaseline(plan);
      // A save REPLACES the draft: the new version carries the edits and the old
      // row is discarded, so the id in the URL is now dead. Point the URL at the
      // new draft before refreshing -- refreshing alone re-runs the page against
      // the discarded id and 404s the editor out from under the user.
      if (result.versionId && result.versionId !== liveVersionId.current) {
        liveVersionId.current = result.versionId;
        router.replace(`/vaccination/plan/edit?version=${result.versionId}`);
      }
      router.refresh();
    });
  }

  function onPublish() {
    setError(null);
    startTransition(async () => {
      // Save first: publishing what is on screen, not what was last saved.
      const savedResult = await saveDraftPlan(liveVersionId.current, plan, liveRuleDsl.current);
      if (!savedResult.ok) {
        setError(savedResult.error);
        return;
      }
      setBaseline(plan);
      // The save REPLACED the draft, so the publish must target the replacement id.
      // Do not navigate before publishing: replacing the route can interrupt this
      // client action and leave the user on a saved-but-unpublished draft.
      const liveId = savedResult.versionId ?? liveVersionId.current;
      liveVersionId.current = liveId;
      const published = await publishPlan(liveId);
      if (!published.ok) {
        setError(published.error);
        if (liveId !== props.draftVersionId) {
          router.replace(`/vaccination/plan/edit?version=${liveId}`);
        }
        router.refresh();
        return;
      }
      router.push("/vaccination/plan");
      router.refresh();
    });
  }

  if (!current) {
    return (
      <PageRoot>
        <Card>
          <CardHeader title="Company vaccination plan" subheader="This draft has no vaccines in it." sx={{ pb: 3 }} />
        </Card>
      </PageRoot>
    );
  }

  const publishBlockedReason = !blockedReason && !props.canPublish ? (props.cannotPublishReason ?? "Your role cannot publish the vaccination plan.") : null;

  return (
    <PageRoot sx={{ pb: 12 }}>
      <PageHeader
        title="Company vaccination plan"
        crumbs={[{ label: "Preventive Care" }, { label: "Vaccination plan", href: "/vaccination/plan" }, { label: props.draftLabel }]}
        actions={
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
            <Label variant="soft" color="warning">
              Draft · not live yet
            </Label>
            <MuiButton
              component={Link}
              href="/vaccination/plan"
              variant="outlined"
              color="inherit"
              startIcon={<Iconify icon="eva:arrow-ios-back-fill" />}
              onClick={(e: React.MouseEvent<HTMLElement>) => {
                if (!dirty) return;
                e.preventDefault();
                setLeaveConfirm(true);
              }}
            >
              Back to plans
            </MuiButton>
          </Stack>
        }
      />
      <Dialog fullWidth maxWidth="xs" open={leaveConfirm} onClose={() => setLeaveConfirm(false)} slotProps={{ paper: { "aria-label": "Leave without saving" } }}>
        <DialogTitle sx={{ pb: 2 }}>Leave without saving?</DialogTitle>
        <DialogContent sx={{ typography: "body2" }}>Your changes to this draft will be lost.</DialogContent>
        <DialogActions>
          <MuiButton color="inherit" variant="outlined" onClick={() => setLeaveConfirm(false)}>
            Stay here
          </MuiButton>
          <MuiButton
            color="error"
            variant="contained"
            onClick={() => {
              setLeaveConfirm(false);
              router.push("/vaccination/plan");
            }}
          >
            Discard changes
          </MuiButton>
        </DialogActions>
      </Dialog>

      <Stack spacing={3}>
        {/* Template invoice-analytic strip: the draft's scope facts. */}
        <Card>
          <Stack direction={{ xs: "column", md: "row" }} divider={<Divider flexItem orientation="vertical" sx={{ borderStyle: "dashed" }} />} sx={{ py: 2 }}>
            <StripCell icon="solar:users-group-rounded-bold" label="Applies to" value={props.scopeType === "park" ? "One park" : "Both parks"} />
            <StripCell icon="solar:medical-kit-bold" label="Vaccines switched on" value={`${onCount} of ${plan.vaccines.length}`} />
            <StripCell
              icon="solar:restart-bold"
              label="Replaces"
              value={props.liveLabel ? `${props.liveLabel}${props.liveSince ? ` · live since ${props.liveSince}` : ""}` : "Nothing — this is the first plan"}
            />
          </Stack>
        </Card>

        {error ? <Alert severity="error">{error}</Alert> : null}

        {/* Template product new/edit: md 4 selector column beside the md 8 form column. */}
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 4 }}>
            <Stack spacing={3}>
              <Card component="nav" aria-label="Vaccines">
                <CardHeader title="Vaccines" subheader={`${onCount} on · ${plan.vaccines.length - onCount} off`} />
                <List sx={{ p: 1.5, display: { xs: "flex", md: "block" }, gap: 1, overflowX: { xs: "auto", md: "visible" } }}>
                  {plan.vaccines.map((v) => (
                    <RailItem
                      key={v.code}
                      selected={!selectedSetting && v.code === current.code}
                      initials={initials(v.name)}
                      primary={v.name}
                      secondary={summarise(v)}
                      muted={!v.on}
                      onClick={() => setSelected(v.code)}
                    />
                  ))}
                </List>
                <Divider sx={{ borderStyle: "dashed" }} />
                <Box sx={{ p: 1.5 }}>
                  <MuiButton fullWidth color="primary" startIcon={<Iconify icon="mingcute:add-line" />} onClick={() => setAddingVaccine(true)}>
                    Add a vaccine
                  </MuiButton>
                </Box>
              </Card>
              <Card component="nav" aria-label="Plan settings">
                <CardHeader title="Plan settings" subheader="Shared rules" />
                <List sx={{ p: 1.5 }}>
                  <RailItem selected={selected === "__procurement"} initials="PH" primary="Procurement holding" secondary="breeding/fattening waves" onClick={() => setSelected("__procurement")} />
                  <RailItem selected={selected === "__safety"} initials="SR" primary="Automatic safety rules" secondary="spacing and defer rules" onClick={() => setSelected("__safety")} />
                </List>
              </Card>
            </Stack>
          </Grid>

          <Grid size={{ xs: 12, md: 8 }}>
            {selected === "__procurement" ? (
              <ProcurementCard plan={plan} setPlan={setPlan} onEdit={() => setSaved(false)} />
            ) : selected === "__safety" ? (
              <SafetyCard plan={plan} />
            ) : (
              <Stack spacing={3}>
                <Card>
                  <CardHeader
                    title={
                      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
                        {current.name}
                        <Label variant="soft">{current.vaccineClass || "—"}</Label>
                      </Box>
                    }
                    subheader={current.disease || undefined}
                    action={
                      <FormControlLabel
                        labelPlacement="start"
                        label={current.on ? "In this plan" : "Switched off"}
                        control={
                          <Switch
                            checked={current.on}
                            slotProps={{ input: { "aria-label": `Include ${current.name}` } }}
                            onChange={() => updateVaccine(current.code, (v) => ({ ...v, on: !v.on }))}
                          />
                        }
                        sx={{ mr: 0, "& .MuiFormControlLabel-label": { typography: "body2", color: "text.secondary" } }}
                      />
                    }
                  />

                  <Stack spacing={3} sx={{ p: 3, opacity: current.on ? 1 : 0.64 }}>
                    {current.on && current.kidDoses.length === 0 && current.driveDoses.length === 0 ? (
                      <Alert severity="warning">This vaccine is in the plan but has no doses yet. Add at least one below, or switch it off.</Alert>
                    ) : null}

                    {current.on && currentAnchorRows.length > 0 ? (
                      <Box>
                        <SectionLabel>Optional anchor/base dates</SectionLabel>
                        <Paper variant="outlined" sx={{ overflow: "hidden" }}>
                          <VaccinationAnchorPanel rows={currentAnchorRows} anchors={current.anchors ?? {}} onChange={updateAnchor} />
                        </Paper>
                      </Box>
                    ) : null}

                    {current.kidDoses.length > 0 || current.on ? (
                      <Box>
                        {current.kidDoses.length > 0 ? (
                          <SectionLabel>{current.kidDoses.length > 1 ? "The doses a young animal gets" : "The dose a young animal gets"}</SectionLabel>
                        ) : null}
                        <Stack spacing={1.5}>
                          {current.kidDoses.map((dose, index) => (
                            <DoseRow key={dose.doseCode} tag={index === 0 ? (current.kidDoses.length > 1 ? "First dose" : "Only dose") : "Booster"} tone={index > 0 ? "info" : "default"}>
                              Give it when the animal is{" "}
                              <DurationField
                                days={dose.offsetDays}
                                title="Give this dose at"
                                disabled={!current.on}
                                onChange={(days) =>
                                  updateVaccine(current.code, (v) => ({
                                    ...v,
                                    kidDoses: v.kidDoses.map((d, i) => (i === index ? { ...d, offsetDays: days } : d)),
                                  }))
                                }
                              />{" "}
                              old
                            </DoseRow>
                          ))}
                          {current.on ? (
                            <AddRow
                              label="Add a dose from date of birth"
                              onClick={() =>
                                updateVaccine(current.code, (v) => ({
                                  ...v,
                                  kidDoses: [
                                    ...v.kidDoses,
                                    {
                                      // A new dose starts three weeks after the last one, the
                                      // minimum booster gap the safety rules enforce anyway.
                                      offsetDays: (v.kidDoses.at(-1)?.offsetDays ?? 0) + 21,
                                      triggerType: "birth_age",
                                      doseCode: `new-kid-${v.kidDoses.length + 1}`,
                                    },
                                  ],
                                }))
                              }
                            />
                          ) : null}
                        </Stack>
                      </Box>
                    ) : null}

                    {current.driveDoses.length > 0 ? (
                      <Box>
                        <SectionLabel>Doses given on a drive</SectionLabel>
                        <Stack spacing={1.5}>
                          {current.driveDoses.map((dose, index) => (
                            <DoseRow key={dose.doseCode} tag={index === 0 ? "First visit" : "Next visit"}>
                              {index === 0 ? "Due " : "Then "}
                              <DurationField
                                days={dose.offsetDays}
                                title={index === 0 ? "Due after the drive starts" : "After the previous dose"}
                                disabled={!current.on}
                                onChange={(days) =>
                                  updateVaccine(current.code, (v) => ({
                                    ...v,
                                    driveDoses: v.driveDoses.map((d, i) => (i === index ? { ...d, offsetDays: days } : d)),
                                  }))
                                }
                              />{" "}
                              {index === 0 ? "after the drive starts" : "after the previous dose"}
                            </DoseRow>
                          ))}
                        </Stack>
                      </Box>
                    ) : null}

                    {current.maxLateDays === null && current.on ? (
                      <AddRow label="Set how late a dose may be" onClick={() => updateVaccine(current.code, (v) => ({ ...v, maxLateDays: 7 }))} />
                    ) : null}

                    {current.maxLateDays !== null ? (
                      <DoseRow tag="Deadline" tone="warning" footer={<Timeline lateDays={current.maxLateDays} />}>
                        Any dose can be up to{" "}
                        <DurationField
                          days={current.maxLateDays}
                          title="Can be given up to … late"
                          disabled={!current.on}
                          onChange={(days) => updateVaccine(current.code, (v) => ({ ...v, maxLateDays: days }))}
                        />{" "}
                        late
                      </DoseRow>
                    ) : null}

                    {current.repeatDays !== null ? (
                      <Box>
                        <SectionLabel>How often to repeat it</SectionLabel>
                        <DoseRow
                          tag="Repeat"
                          tone="primary"
                          footer={
                            <Box role="group" aria-label="Common intervals" sx={{ display: "flex", flexWrap: "wrap", gap: 1, mt: 2 }}>
                              {/* Values the picker can express exactly, so clicking a preset and then reading the chip agree. */}
                              {[90, 180, 270, 365, 1095].map((days) => (
                                <Chip
                                  key={days}
                                  label={humanDays(days)}
                                  color={current.repeatDays === days ? "primary" : "default"}
                                  variant={current.repeatDays === days ? "filled" : "outlined"}
                                  aria-pressed={current.repeatDays === days}
                                  disabled={!current.on}
                                  onClick={() => updateVaccine(current.code, (v) => ({ ...v, repeatDays: days }))}
                                />
                              ))}
                            </Box>
                          }
                        >
                          Do it again every{" "}
                          <DurationField
                            days={current.repeatDays}
                            title="Repeat every"
                            disabled={!current.on}
                            onChange={(days) => updateVaccine(current.code, (v) => ({ ...v, repeatDays: days }))}
                          />{" "}
                          after the last dose was given.
                        </DoseRow>
                      </Box>
                    ) : current.on ? (
                      <AddRow label="Make it repeat" onClick={() => updateVaccine(current.code, (v) => ({ ...v, repeatDays: 365 }))} />
                    ) : null}
                  </Stack>
                </Card>

                <ProofCard mode={plan.proofMode} />
                <ImpactCard impact={props.impact} />
              </Stack>
            )}
          </Grid>
        </Grid>
      </Stack>

      {/* Pinned action bar: publishing is decided after reading the whole draft, not before. */}
      <Paper
        elevation={0}
        sx={{
          position: "sticky",
          bottom: 16,
          zIndex: 5,
          mt: 3,
          p: 2,
          gap: 1.5,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          pr: { xs: 2, sm: 11 },
          boxShadow: (theme) => theme.vars.customShadows.z16,
        }}
      >
        <Label variant="soft" color="warning">
          {props.draftLabel} · draft
        </Label>
        {saved && !dirty ? (
          <Typography variant="body2" sx={{ color: "success.main" }} role="status">
            Draft saved.
          </Typography>
        ) : null}
        {blockedReason ? (
          <Typography variant="body2" sx={{ color: "warning.main" }}>
            {blockedReason}
          </Typography>
        ) : null}
        {publishBlockedReason ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {publishBlockedReason}
          </Typography>
        ) : null}
        <Box sx={{ flexGrow: 1 }} />
        <MuiButton
          variant="outlined"
          color="inherit"
          disabled={pending || !dirty}
          onClick={() => {
            setPlan(props.initialPlan);
            setSaved(false);
          }}
        >
          Reset
        </MuiButton>
        <MuiButton variant="outlined" color="inherit" disabled={pending || !dirty || blockedReason !== null} title={blockedReason ?? undefined} onClick={onSave}>
          {pending ? "Working…" : "Save draft"}
        </MuiButton>
        <MuiButton
          variant="contained"
          color="primary"
          disabled={pending || blockedReason !== null || !props.canPublish}
          title={blockedReason ?? (props.canPublish ? undefined : (props.cannotPublishReason ?? "Your role cannot publish the vaccination plan."))}
          onClick={onPublish}
        >
          Publish plan
        </MuiButton>
      </Paper>

      <AddVaccineModal open={addingVaccine} onSave={onAddVaccine} onCancel={() => setAddingVaccine(false)} />
    </PageRoot>
  );
}

// ---------------------------------------------------------------------------------------------
// Template parts used above.

function StripCell({ icon, label, value }: { icon: IconifyName; label: string; value: string }) {
  return (
    <Box sx={{ flex: "1 1 0", px: 3, py: { xs: 1, md: 0 }, display: "flex", alignItems: "center", gap: 2, minWidth: 0 }}>
      <Avatar sx={{ width: "calc(6 * var(--spacing))", height: "calc(6 * var(--spacing))", bgcolor: "background.neutral", color: "primary.main" }}>
        <Iconify icon={icon} width={24} />
      </Avatar>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {label}
        </Typography>
        <Typography variant="subtitle1" sx={{ overflowWrap: "anywhere" }}>
          {value}
        </Typography>
      </Box>
    </Box>
  );
}

function RailItem({ selected, initials: text, primary, secondary, muted, onClick }: { selected: boolean; initials: string; primary: string; secondary: string; muted?: boolean; onClick: () => void }) {
  return (
    <ListItemButton
      selected={selected}
      aria-current={selected}
      onClick={onClick}
      sx={{ borderRadius: 0.75, gap: 1.5, py: 1, flexShrink: 0, minWidth: { xs: 220, md: 0 }, opacity: muted ? 0.56 : 1 }}
    >
      <Avatar variant="rounded" sx={{ width: "calc(4.5 * var(--spacing))", height: "calc(4.5 * var(--spacing))", typography: "subtitle2", bgcolor: selected ? "primary.main" : "background.neutral", color: selected ? "primary.contrastText" : "text.secondary" }}>
        {text}
      </Avatar>
      <ListItemText
        primary={primary}
        secondary={secondary}
        slotProps={{ primary: { sx: { typography: "subtitle2" } }, secondary: { sx: { typography: "caption" } } }}
      />
    </ListItemButton>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="overline" component="h3" sx={{ display: "block", mb: 1.5, color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

function DoseRow({ tag, tone = "default", children, footer }: { tag: string; tone?: LabelColor; children: React.ReactNode; footer?: React.ReactNode }) {
  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap", typography: "body2" }}>
        <Label variant="soft" color={tone} sx={{ minWidth: 88 }}>
          {tag}
        </Label>
        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", flexWrap: "wrap", gap: 0.5 }}>
          {children}
        </Box>
      </Box>
      {footer}
    </Paper>
  );
}

function AddRow({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <MuiButton fullWidth variant="outlined" color="inherit" startIcon={<Iconify icon="mingcute:add-line" />} onClick={onClick} sx={{ borderStyle: "dashed", color: "text.secondary" }}>
      {label}
    </MuiButton>
  );
}

/**
 * "Add a vaccine to this plan" — the rail's "Add a vaccine" action, as a template Dialog (portal,
 * backdrop, Escape). Every duration is a DurationField, never a canned list (MOCK-BEHAVIOUR-SPEC §6);
 * live/killed, bacterial/viral, species and one-dose-or-two are finite enumerations (MUI selects).
 */
function AddVaccineModal({ open, onSave, onCancel }: { open: boolean; onSave: (input: NewVaccineInput) => string | null; onCancel: () => void }) {
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [disease, setDisease] = useState("");
  const [vaccineType, setVaccineType] = useState<"live" | "killed">("killed");
  const [pathogenClass, setPathogenClass] = useState<"bacterial" | "viral">("bacterial");
  const [species, setSpecies] = useState<"goat" | "sheep" | "both">("both");
  const [procurementPurpose, setProcurementPurpose] = useState<"all" | "breeding" | "fattening" | "non_breeding">("all");
  const [courseType, setCourseType] = useState<"single" | "booster">("single");
  const [firstDoseDays, setFirstDoseDays] = useState(84); // 12 weeks, the mock's own default
  const [boosterGapDays, setBoosterGapDays] = useState(21); // 3 weeks
  const [repeats, setRepeats] = useState(true);
  const [repeatDays, setRepeatDays] = useState(365);
  const [maxLateDays, setMaxLateDays] = useState(14); // 2 weeks
  const [err, setErr] = useState<string | null>(null);

  function handleSave() {
    const result = onSave({
      name,
      code,
      disease,
      vaccineType,
      pathogenClass,
      species,
      procurementPurpose,
      courseType,
      firstDoseDays,
      boosterGapDays,
      repeatDays: repeats ? repeatDays : null,
      maxLateDays,
    });
    if (result) setErr(result);
  }

  const grid = { display: "grid", gap: 2.5, gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" } } as const;
  const selectProps = { inputLabel: { shrink: true }, select: { MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } } as const;

  return (
    <Dialog open={open} onClose={onCancel} fullWidth maxWidth="md" data-testid="vp-add-vaccine" slotProps={{ paper: { "aria-label": "Add a vaccine to this plan" } }}>
      <DialogTitle sx={{ pr: 7 }}>
        <Typography variant="overline" component="div" sx={{ color: "text.secondary" }}>
          New vaccine
        </Typography>
        Add a vaccine to this plan
        <IconButton onClick={onCancel} aria-label="Close" sx={{ position: "absolute", top: 12, right: 12 }}>
          <Iconify icon="mingcute:close-line" />
        </IconButton>
      </DialogTitle>
      <DialogContent>
        <Stack spacing={3} sx={{ pt: 1 }}>
          <Box>
            <SectionLabel>Basics</SectionLabel>
            <Box sx={grid}>
              <MuiTextField autoFocus required label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Brucella" slotProps={{ inputLabel: { shrink: true } }} />
              <MuiTextField required label="Short code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="e.g. BRU" slotProps={{ inputLabel: { shrink: true } }} />
              <MuiTextField label="What it protects against" value={disease} onChange={(e) => setDisease(e.target.value)} placeholder="e.g. Brucellosis" slotProps={{ inputLabel: { shrink: true } }} sx={{ gridColumn: { sm: "1 / -1" } }} />
            </Box>
          </Box>
          <Box>
            <SectionLabel>Biology — this decides the spacing rules</SectionLabel>
            <Box sx={grid}>
              <MuiTextField select required label="Live or killed" value={vaccineType} onChange={({ target: { value: next } }) => setVaccineType(next as "live" | "killed")} slotProps={selectProps}>
                <MenuItem value="killed">Killed</MenuItem>
                <MenuItem value="live">Live</MenuItem>
              </MuiTextField>
              <MuiTextField select required label="Bacterial or viral" value={pathogenClass} onChange={({ target: { value: next } }) => setPathogenClass(next as "bacterial" | "viral")} slotProps={selectProps}>
                <MenuItem value="bacterial">Bacterial</MenuItem>
                <MenuItem value="viral">Viral</MenuItem>
              </MuiTextField>
            </Box>
          </Box>
          <Box>
            <SectionLabel>Who gets it</SectionLabel>
            <Box sx={grid}>
              <MuiTextField select required label="Species" value={species} onChange={({ target: { value: next } }) => setSpecies(next as "goat" | "sheep" | "both")} slotProps={selectProps}>
                <MenuItem value="both">Goats and sheep</MenuItem>
                <MenuItem value="goat">Goats only</MenuItem>
                <MenuItem value="sheep">Sheep only</MenuItem>
              </MuiTextField>
              <Box>
                <Typography variant="caption" component="div" sx={{ color: "text.secondary", mb: 0.75 }}>
                  Procurement purpose
                </Typography>
                <ToggleButtonGroup
                  exclusive
                  size="small"
                  aria-label="Procurement purpose"
                  value={procurementPurpose}
                  onChange={(_, value) => {
                    if (value) setProcurementPurpose(value as "all" | "breeding" | "fattening" | "non_breeding");
                  }}
                  sx={{ flexWrap: "wrap" }}
                >
                  {[
                    ["all", "All purposes"],
                    ["breeding", "Breeding"],
                    ["fattening", "Fattening"],
                    ["non_breeding", "Non-breeding"],
                  ].map(([value, label]) => (
                    <ToggleButton key={value} value={value} aria-pressed={procurementPurpose === value}>
                      {label}
                    </ToggleButton>
                  ))}
                </ToggleButtonGroup>
              </Box>
            </Box>
          </Box>
          <Box>
            <SectionLabel>The course</SectionLabel>
            <Box sx={grid}>
              <MuiTextField select required label="One dose or two" value={courseType} onChange={({ target: { value: next } }) => setCourseType(next as "single" | "booster")} slotProps={selectProps}>
                <MenuItem value="single">One dose only</MenuItem>
                <MenuItem value="booster">First dose + booster</MenuItem>
              </MuiTextField>
              <FieldRow label="First dose at">
                <DurationField days={firstDoseDays} onChange={setFirstDoseDays} title="First dose at" />
              </FieldRow>
              {courseType === "booster" ? (
                <FieldRow label="Booster, after the first dose">
                  <DurationField days={boosterGapDays} onChange={setBoosterGapDays} title="Booster, after the first dose" />
                </FieldRow>
              ) : null}
              <FieldRow label="Repeat every">
                {repeats ? (
                  <>
                    <DurationField days={repeatDays} onChange={setRepeatDays} title="Repeat every" />
                    <MuiButton size="small" color="inherit" onClick={() => setRepeats(false)}>
                      Does not repeat
                    </MuiButton>
                  </>
                ) : (
                  <MuiButton size="small" color="primary" startIcon={<Iconify icon="mingcute:add-line" />} onClick={() => setRepeats(true)}>
                    Make it repeat
                  </MuiButton>
                )}
              </FieldRow>
              <FieldRow label="Can be given up to … late">
                <DurationField days={maxLateDays} onChange={setMaxLateDays} title="Can be given up to … late" />
              </FieldRow>
            </Box>
          </Box>
          {err ? <Alert severity="error">{err}</Alert> : null}
        </Stack>
      </DialogContent>
      <DialogActions>
        <MuiButton variant="outlined" color="inherit" onClick={onCancel}>
          Cancel
        </MuiButton>
        <MuiButton variant="contained" color="primary" onClick={handleSave}>
          Add to the draft
        </MuiButton>
      </DialogActions>
    </Dialog>
  );
}

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Box>
      <Typography variant="caption" component="div" sx={{ color: "text.secondary", mb: 0.75 }}>
        {label}
      </Typography>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>{children}</Box>
    </Box>
  );
}

/**
 * The deadline, drawn. Success is the due day, warning the window in which a late dose is still
 * accepted. No segment before the due day: the window runs forward only.
 */
function Timeline({ lateDays }: { lateDays: number }) {
  return (
    <Box sx={{ mt: 2 }}>
      <Box sx={{ display: "flex", borderRadius: 0.75, overflow: "hidden", typography: "caption", fontWeight: "fontWeightSemiBold" }}>
        <Box sx={{ flex: 1, py: 0.75, textAlign: "center", bgcolor: "success.main", color: "success.contrastText" }}>Due day</Box>
        <Box sx={{ flex: 3, py: 0.75, textAlign: "center", bgcolor: "warning.main", color: "warning.contrastText" }}>Still accepted — up to {formatDays(lateDays)} late</Box>
      </Box>
      <Typography variant="caption" component="p" sx={{ mt: 1, color: "text.secondary" }}>
        After <b>{formatDays(lateDays)}</b> the task is missed and goes to Preventive Care review.
      </Typography>
    </Box>
  );
}

function ProcurementCard({ plan, setPlan, onEdit }: { plan: EditorPlan; setPlan: (fn: (p: EditorPlan) => EditorPlan) => void; onEdit: () => void }) {
  const { warmupNoVaccinationDays, kidsNormalScheduleUntilWeeks, adultPriorVaccinationAllowed } = plan.procurement;
  const [activePurpose, setActivePurpose] = useState<ProcurementPurpose>("breeding");
  const procurementPurposeOptions = [
    ["breeding", "Breeding · 843"],
    ["fattening", "Fattening · 713"],
  ] as const;
  const vaccineChips = plan.vaccines.filter((v) => v.on).map((v) => ({ code: v.code, name: v.name }));
  const activePurposePlan = plan.procurement.purposePlans[activePurpose];
  const firstWave = activePurposePlan.firstWave;
  const goatSecondWave = activePurposePlan.goatSecondWave;
  const sheepSecondWave = activePurposePlan.sheepSecondWave;

  return (
    <Card>
      <CardHeader
        title="Procurement holding"
        subheader="Only vaccines given by us, in our parks or a supervised procurement holding park with proof, count as trusted history. Anything else starts the normal schedule once the animal reaches our pens."
      />
      <Stack spacing={2.5} sx={{ p: 3 }}>
        {warmupNoVaccinationDays !== null ? (
          <DoseRow tag="Settle in">
            After it arrives, give no vaccine for{" "}
            <DurationField
              days={warmupNoVaccinationDays}
              title="Settle-in period"
              onChange={(days) => {
                onEdit();
                setPlan((p) => ({ ...p, procurement: { ...p.procurement, warmupNoVaccinationDays: days } }));
              }}
            />
            .
          </DoseRow>
        ) : null}

        {kidsNormalScheduleUntilWeeks !== null ? (
          <DoseRow tag="Kid or adult">
            Anything younger than{" "}
            <DurationField
              days={kidsNormalScheduleUntilWeeks * 7}
              title="Still counts as a kid until"
              onChange={(days) => {
                onEdit();
                setPlan((p) => ({ ...p, procurement: { ...p.procurement, kidsNormalScheduleUntilWeeks: Math.max(1, Math.round(days / 7)) } }));
              }}
            />{" "}
            follows the normal kid schedule above. Older animals use the waves below.
          </DoseRow>
        ) : null}

        {adultPriorVaccinationAllowed !== null ? (
          <DoseRow tag="Prior doses">
            Vaccinations the seller claims they already gave:
            <ToggleButtonGroup
              exclusive
              size="small"
              aria-label="Prior doses"
              value={adultPriorVaccinationAllowed ? "count" : "ignore"}
              onChange={(_, value) => {
                if (!value) return;
                onEdit();
                setPlan((p) => ({ ...p, procurement: { ...p.procurement, adultPriorVaccinationAllowed: value === "count" } }));
              }}
              sx={{ ml: 1 }}
            >
              <ToggleButton value="count" aria-pressed={adultPriorVaccinationAllowed}>
                Count them
              </ToggleButton>
              <ToggleButton value="ignore" aria-pressed={!adultPriorVaccinationAllowed}>
                Ignore them
              </ToggleButton>
            </ToggleButtonGroup>
          </DoseRow>
        ) : null}

        <Box>
          <SectionLabel>What we bought them for</SectionLabel>
          <ToggleButtonGroup
            exclusive
            size="small"
            aria-label="Procurement animal purpose"
            value={activePurpose}
            onChange={(_, value) => {
              if (value) setActivePurpose(value as ProcurementPurpose);
            }}
          >
            {procurementPurposeOptions.map(([value, label]) => (
              <ToggleButton key={value} value={value} aria-pressed={activePurpose === value}>
                {label}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
          <Typography variant="body2" sx={{ mt: 1.5, color: "text.secondary" }}>
            Breeding stock stays for years, so it needs the full schedule. Fattening animals are sold before most repeats come round, so long-interval vaccines can waste doses.
          </Typography>
        </Box>

        <ProcurementWave
          title={activePurpose === "fattening" ? "First wave for fattening animals" : "First wave for breeding stock"}
          vaccines={vaccineChips}
          selected={firstWave}
          onToggle={(vaccine) => {
            onEdit();
            setPlan((p) => ({
              ...p,
              procurement: {
                ...p.procurement,
                purposePlans: updatePurposePlan(p.procurement.purposePlans, activePurpose, {
                  firstWave: toggleVaccineSelection(p.procurement.purposePlans[activePurpose].firstWave, vaccine),
                }),
              },
            }));
          }}
        />

        {activePurposePlan.secondWaveAfterDays !== null ? (
          <DoseRow tag="Gap">
            Wait{" "}
            <DurationField
              days={activePurposePlan.secondWaveAfterDays}
              title="Second wave gap"
              onChange={(days) => {
                onEdit();
                setPlan((p) => ({
                  ...p,
                  procurement: { ...p.procurement, purposePlans: updatePurposePlan(p.procurement.purposePlans, activePurpose, { secondWaveAfterDays: days }) },
                }));
              }}
            />{" "}
            after the first wave before the second visit.
          </DoseRow>
        ) : null}

        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          Live vaccines in the second wave still wait out the live-to-live spacing window. ET + TT dose 2 uses its own course gap.
        </Typography>

        <Box sx={{ display: "grid", gap: 2.5, gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" } }}>
          <ProcurementWave
            title="Goat second wave"
            vaccines={vaccineChips}
            selected={goatSecondWave}
            onToggle={(vaccine) => {
              onEdit();
              setPlan((p) => ({
                ...p,
                procurement: {
                  ...p.procurement,
                  purposePlans: updatePurposePlan(p.procurement.purposePlans, activePurpose, {
                    goatSecondWave: toggleVaccineSelection(p.procurement.purposePlans[activePurpose].goatSecondWave, vaccine),
                  }),
                },
              }));
            }}
          />
          <ProcurementWave
            title="Sheep second wave"
            vaccines={vaccineChips}
            selected={sheepSecondWave}
            onToggle={(vaccine) => {
              onEdit();
              setPlan((p) => ({
                ...p,
                procurement: {
                  ...p.procurement,
                  purposePlans: updatePurposePlan(p.procurement.purposePlans, activePurpose, {
                    sheepSecondWave: toggleVaccineSelection(p.procurement.purposePlans[activePurpose].sheepSecondWave, vaccine),
                  }),
                },
              }));
            }}
          />
        </Box>
      </Stack>
    </Card>
  );
}

function ProcurementWave({ title, vaccines, selected, onToggle }: { title: string; vaccines: Array<{ code: string; name: string }>; selected: string[]; onToggle: (vaccineName: string) => void }) {
  if (vaccines.length === 0) return null;
  const active = new Set(selected.map((item) => normaliseVaccineName(item)));
  const isActive = (vaccine: { code: string; name: string }) => active.has(normaliseVaccineName(vaccine.code)) || active.has(normaliseVaccineName(vaccine.name));
  return (
    <Box>
      <SectionLabel>{title}</SectionLabel>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
        {vaccines.map((vaccine) => (
          <Chip
            key={vaccine.code}
            label={vaccine.name}
            color={isActive(vaccine) ? "primary" : "default"}
            variant={isActive(vaccine) ? "filled" : "outlined"}
            aria-pressed={isActive(vaccine)}
            icon={isActive(vaccine) ? <Iconify icon="eva:checkmark-fill" width={16} /> : undefined}
            onClick={() => onToggle(vaccine.name)}
          />
        ))}
      </Box>
    </Box>
  );
}

function updatePurposePlan(
  purposePlans: EditorPlan["procurement"]["purposePlans"],
  purpose: ProcurementPurpose,
  patch: Partial<EditorPlan["procurement"]["purposePlans"][ProcurementPurpose]>,
): EditorPlan["procurement"]["purposePlans"] {
  return {
    ...purposePlans,
    [purpose]: {
      ...purposePlans[purpose],
      ...patch,
    },
  };
}

function toggleVaccineSelection(selected: string[], vaccineName: string): string[] {
  const normalized = normaliseVaccineName(vaccineName);
  const exists = selected.some((item) => normaliseVaccineName(item) === normalized);
  if (exists) {
    return selected.filter((item) => normaliseVaccineName(item) !== normalized);
  }
  return [...selected, vaccineName];
}

function normaliseVaccineName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, "");
}

function ruleRowsForVaccine(vaccine: EditorVaccine | undefined): Array<{
  vaccine: { code: string; name: string };
  rule: ScheduleRule;
}> {
  if (!vaccine) return [];
  const vaccineRef = { code: vaccine.code, name: vaccine.name };
  const firstDoseRules = [...vaccine.kidDoses, ...vaccine.driveDoses]
    .filter((dose) => dose.doseCode)
    .map((dose, index) => ({
      vaccine: vaccineRef,
      rule: {
        dose_code: dose.doseCode,
        source_dose_code: dose.doseCode,
        offset_days: dose.offsetDays,
        trigger_type: dose.triggerType,
        repeat: "none",
        sequence: index + 1,
      } as ScheduleRule,
    }));
  if (vaccine.repeatDays === null || !vaccine.repeatDoseCode) return firstDoseRules;
  return [
    ...firstDoseRules,
    {
      vaccine: vaccineRef,
      rule: {
        dose_code: vaccine.repeatDoseCode,
        source_dose_code: vaccine.repeatDoseCode,
        offset_days: vaccine.repeatDays,
        trigger_type: "after_previous_completion",
        repeat: "every_n_days",
        min_gap_days: vaccine.repeatDays,
        sequence: firstDoseRules.length + 1,
      } as ScheduleRule,
    },
  ];
}

/**
 * The rules that run whatever this plan says. Read-only and read from the document, not typed in
 * here: a hardcoded list would keep displaying "28 days" after someone changed the stored gap.
 */
function SafetyCard({ plan }: { plan: EditorPlan }) {
  const s = plan.safety;
  const lines: string[] = [];
  if (s.maxVaccinesPerSession) lines.push(`At most ${s.maxVaccinesPerSession} vaccines per animal per visit.`);
  if (s.liveToLiveGapDays) lines.push(`Live to live: at least ${humanDays(s.liveToLiveGapDays)} apart.`);
  if (s.liveToKilledGapDays && s.killedToKilledGapDays) {
    lines.push(`Live to killed and killed to killed: at least ${humanDays(s.liveToKilledGapDays)} apart unless the same-day rule allows it.`);
  }
  if (s.kidBoosterMinGapDays) lines.push(`A booster is never closer than ${humanDays(s.kidBoosterMinGapDays)} to its first dose.`);
  if (s.skipFromPregnancyMonth && s.skipThroughPregnancyMonth) {
    lines.push(
      `Pregnancy months ${s.skipFromPregnancyMonth} and ${s.skipThroughPregnancyMonth} skip vaccination${
        s.postDeliveryCatchUpDays ? `; catch up within ${humanDays(s.postDeliveryCatchUpDays)} after delivery` : ""
      }.`,
    );
  }
  if (s.deferStates.length > 0) lines.push(`Deferred while: ${s.deferStates.join(", ").replace(/_/g, " ")}.`);
  if (s.maxBatchingHoldDays) lines.push(`A drive may wait up to ${humanDays(s.maxBatchingHoldDays)} to batch; the safety window always wins.`);

  if (lines.length === 0) return null;

  return (
    <Card>
      <CardHeader
        title={
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            Automatic safety rules <Label variant="soft">read-only</Label>
          </Box>
        }
        subheader="These are part of the plan and run after everything above, on every vaccine in it."
      />
      <Stack component="ul" spacing={1.5} sx={{ p: 3, m: 0, listStyle: "none" }}>
        {lines.map((line) => (
          <Box component="li" key={line} sx={{ display: "flex", alignItems: "flex-start", gap: 1.5, typography: "body2" }}>
            <Iconify icon="solar:check-circle-bold" width={20} sx={{ color: "success.main", flexShrink: 0 }} />
            {line}
          </Box>
        ))}
      </Stack>
    </Card>
  );
}

/**
 * How the operator proves it. Read-only: the choice lives on the version's proof policy, which the
 * editor does not write, so this states what is in force (template radio-card anatomy, disabled).
 */
function ProofCard({ mode }: { mode: "shed" | "animal" | null }) {
  if (!mode) return null;
  const options = [
    { value: "shed", title: "One video per pen", body: "The operator scans every animal's tag as it is done, then records one video covering the whole pen. A 200-animal pen produces 1 clip." },
    { value: "animal", title: "One video per animal", body: "The operator scans a tag and records that animal's injection, one at a time. A 200-animal pen produces 200 clips." },
  ] as const;
  return (
    <Card>
      <CardHeader
        title={
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            How the operator proves it <Label variant="soft">read-only</Label>
          </Box>
        }
      />
      <Box sx={{ p: 3, display: "grid", gap: 2, gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" } }}>
        {options.map((option) => {
          const on = mode === option.value;
          return (
            <Paper
              key={option.value}
              variant="outlined"
              aria-pressed={on}
              sx={{ p: 2.5, borderColor: on ? "primary.main" : "divider", borderWidth: on ? 2 : 1 }}
            >
              <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
                <Radio checked={on} disabled size="small" sx={{ p: 0 }} slotProps={{ input: { "aria-label": option.title } }} />
                <Typography variant="subtitle2">{option.title}</Typography>
              </Box>
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                {option.body}
              </Typography>
            </Paper>
          );
        })}
      </Box>
    </Card>
  );
}

/**
 * What publishing will change, measured against the CURRENT herd by the backend's own eligibility
 * rollup (the same arithmetic the drive planner uses).
 */
function ImpactCard({ impact }: { impact: ImpactSummary | null }) {
  if (!impact) return null;
  const overCap = Boolean(impact.capacityStatus && impact.capacityStatus !== "within_cap");
  const cells = [
    { key: "animals", value: impact.eligibleAnimals.toLocaleString("en-IN"), label: "animals in scope", warn: false },
    { key: "pens", value: String(impact.affectedSheds), label: "pens affected", warn: false },
    { key: "days", value: String(impact.estimatedDays), label: `operator-days at ${impact.dailyCap}/day`, warn: overCap },
  ];
  return (
    <Card>
      <CardHeader title="What publishing will change" />
      <Box sx={{ p: 3 }}>
        <Paper variant="outlined" sx={{ borderStyle: "dashed" }}>
          <Stack direction={{ xs: "column", sm: "row" }} divider={<Divider flexItem orientation="vertical" sx={{ borderStyle: "dashed" }} />}>
            {cells.map((cell) => (
              <Box key={cell.key} sx={{ flex: "1 1 0", p: 2.5 }}>
                <Typography variant="h4" sx={{ color: cell.warn ? "warning.main" : "text.primary" }}>
                  {cell.value}
                </Typography>
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {cell.label}
                </Typography>
              </Box>
            ))}
          </Stack>
        </Paper>
        {overCap ? (
          <Alert severity="warning" sx={{ mt: 2 }}>
            This does not fit in one day at the current cap of <b>{impact.dailyCap}</b> animals. It will be split across <b>{impact.estimatedDays}</b> days inside the safe window.
          </Alert>
        ) : null}
      </Box>
    </Card>
  );
}

function initials(name: string): string {
  const cleaned = name.replace(/[^A-Za-z0-9+ ]/g, " ").trim();
  const parts = cleaned.split(/[\s+]+/).filter(Boolean);
  if (parts.length >= 2) return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
  return cleaned.slice(0, 2).toUpperCase();
}

function summarise(v: EditorVaccine): string {
  if (!v.on) return "Not in this plan";
  const bits: string[] = [];
  if (v.kidDoses.length > 0) bits.push(`from ${formatDays(v.kidDoses[0].offsetDays)}`);
  if (v.driveDoses.length > 0) bits.push(`${v.driveDoses.length} on a drive`);
  if (v.repeatDays !== null) bits.push(`repeats every ${humanDays(v.repeatDays)}`);
  return bits.join(" · ") || "In this plan";
}
