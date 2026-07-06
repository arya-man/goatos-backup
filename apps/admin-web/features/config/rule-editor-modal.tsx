"use client";

import { useMemo, useState, useTransition } from "react";
import {
  AlertTriangle,
  CalendarDays,
  CheckCircle2,
  ChevronRight,
  Copy,
  Info,
  Pencil,
  Plus,
  ShieldCheck,
  Syringe,
  X,
} from "lucide-react";
import {
  publishVersions,
  runImpactPreview,
  saveDraftBatch,
  type ActionResult,
} from "./config-actions";
import {
  buildVaccinationMatrixPreview,
  buildRuleDsl,
  hasProofRequirement,
  ALL_STAGES_VALUE,
  parseScope,
  newCompatibilityPolicy,
  newFeedFields,
  newPregnancyPolicy,
  newProcurementPolicy,
  type AnimalStageOption,
  type CompatibilityPolicy,
  type DoseRow,
  type FeedFields,
  type PregnancyPolicy,
  type ProcurementPolicy,
  type RuleInput,
  type SopVersionOption,
  type VaccinationMatrixRow,
} from "./rule-dsl";
import type { ImpactPreviewResult } from "@/lib/api/server";
import {
  copy,
  optionalCopy,
  optionGroup,
  optionLabel,
  optionalOptionGroup,
  type AdminUiOption,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";

type SourceVaccinePreset = {
  code: string;
  name: string;
  vaccineType: string;
  pathogenClass: string;
  courseType: string;
  disease: string;
  compatibilityGroup: string;
  approvedKidWeeks: number[];
  revaccinationDays: number;
  doseAmount: number;
  vialDoses: number;
  priority?: number;
  species: string;
};

type InfoPanel = {
  title: string;
  body: string;
  top: number;
  left: number;
};

const VACCINATION_MATRIX_CODE = "vaccination.matrix";

export function RuleEditorModal({
  open,
  presentation = "modal",
  onClose,
  initialCategory,
  sopVersions = [],
  animalStages = [],
  stagesError = null,
  sopsError = null,
  canPublish,
  publishDisabledReason,
  pageContract,
}: {
  open: boolean;
  presentation?: "modal" | "page";
  onClose: () => void;
  initialCategory: string;
  sopVersions?: SopVersionOption[];
  animalStages?: AnimalStageOption[];
  stagesError?: string | null;
  sopsError?: string | null;
  canPublish: boolean;
  publishDisabledReason: string;
  pageContract: AdminUiPageContract;
}) {
  const ruleCategories = optionGroup(pageContract, "rule_categories");
  const ruleScopes = optionGroup(pageContract, "rule_scopes");
  const scopeOptions = ruleScopes;
  const protocolPlaceholders = optionGroup(
    pageContract,
    "protocol_placeholders",
  );
  const animalStageScope = optionGroup(pageContract, "animal_stage_scope");
  const speciesOptions = optionGroup(pageContract, "rule_species");
  const sexOptions = optionGroup(pageContract, "rule_sexes");
  const breedOptions = optionGroup(pageContract, "rule_breeds");
  const healthOptions = optionGroup(pageContract, "rule_healths");
  const lifecycleOptions = optionGroup(pageContract, "rule_lifecycles");
  const reproductiveOptions = optionGroup(pageContract, "rule_reproductive");
  const deferOptions = optionGroup(pageContract, "defer_states");
  const missedDoseOptions = optionGroup(pageContract, "missed_dose_policies");
  const vaccineTypeOptions = optionGroup(pageContract, "vaccine_types");
  const pathogenClassOptions = optionGroup(
    pageContract,
    "vaccine_pathogen_classes",
  );
  const courseTypeOptions = optionGroup(pageContract, "vaccine_course_types");
  const sourceVaccinePresetOptions = optionGroup(
    pageContract,
    "source_vaccine_matrix_presets",
  );
  const procurementVaccineChoices = optionGroup(
    pageContract,
    "procurement_wave_vaccine_options",
  );
  const proofTokenOptions = optionGroup(pageContract, "proof_requirement_tokens");
  const excludedReproductiveOptions = optionGroup(
    pageContract,
    "excluded_reproductive_states",
  );
  const doseUnitOptions = optionGroup(pageContract, "dose_units");
  const routeSiteOptions = optionGroup(pageContract, "route_sites");
  const courseLapseOptions = optionGroup(pageContract, "course_lapse_policies");
  const triggerOptions = optionGroup(pageContract, "trigger_types");
  const repeatOptions = optionGroup(pageContract, "repeat_policies");
  const catchUpOptions = optionGroup(pageContract, "catch_up_policies");
  const scheduleSopOptions = optionGroup(pageContract, "schedule_sop_labels");
  const feedClassOptions = optionalOptionGroup(pageContract, "feed_classes");
  const feedItemOptions = optionalOptionGroup(pageContract, "feed_items");
  const feedUnitOptions = optionalOptionGroup(pageContract, "feed_units");
  const feedInventoryOptions = optionalOptionGroup(
    pageContract,
    "feed_inventory_policies",
  );
  const feedSourceTableOptions = optionalOptionGroup(
    pageContract,
    "feed_source_tables",
  );
  const feedParameterFamilyOptions = optionalOptionGroup(
    pageContract,
    "feed_parameter_families",
  );
  const feedDimensionOptions = optionalOptionGroup(
    pageContract,
    "feed_dimension_keys",
  );
  const feedRatioOptions = optionalOptionGroup(
    pageContract,
    "feed_ratio_policies",
  );
  const feedValidationOptions = optionalOptionGroup(
    pageContract,
    "feed_validation_checks",
  );
  const feedCalculationOptions = optionalOptionGroup(
    pageContract,
    "feed_calculation_outputs",
  );
  const supportsFeedDirection =
    ruleCategories.some((option) => option.key === "feed_direction") &&
    feedClassOptions.length > 0 &&
    feedItemOptions.length > 0 &&
    feedUnitOptions.length > 0 &&
    feedInventoryOptions.length > 0 &&
    feedSourceTableOptions.length > 0 &&
    feedParameterFamilyOptions.length > 0 &&
    feedDimensionOptions.length > 0 &&
    feedRatioOptions.length > 0 &&
    feedValidationOptions.length > 0 &&
    feedCalculationOptions.length > 0;
  const visibleRuleCategories = ruleCategories.filter(
    (option) => option.key !== "feed_direction" || supportsFeedDirection,
  );

  function firstKey(options: AdminUiOption[], groupId: string): string {
    const [first] = options;
    if (!first)
      throw new Error(
        `Admin-web page contract ${pageContract.route_id} has empty option group ${groupId}`,
      );
    return first.key;
  }
  function firstKeyOrEmpty(options: AdminUiOption[]): string {
    return options[0]?.key ?? "";
  }
  function requireKey(
    options: AdminUiOption[],
    key: string,
    groupId: string,
  ): string {
    if (!options.some((option) => option.key === key)) {
      throw new Error(
        `Admin-web page contract ${pageContract.route_id} missing option ${groupId}.${key}`,
      );
    }
    return key;
  }
  function firstKeyOr(options: AdminUiOption[], fallback: string): string {
    return options[0]?.key ?? fallback;
  }
  function categoryDefault(): string {
    const fallback =
      visibleRuleCategories.find((option) => option.key === "vaccination")
        ?.key ??
      visibleRuleCategories[0]?.key ??
      initialCategory;
    if (initialCategory === "feed_direction" && !supportsFeedDirection)
      return fallback;
    return visibleRuleCategories.some(
      (option) => option.key === initialCategory,
    )
      ? initialCategory
      : fallback;
  }
  function defaultEscalation(categoryKey: string): string {
    return categoryKey === "feed_direction" && supportsFeedDirection
      ? (optionalCopy(
          pageContract,
          "modal.rule_editor.default_feed_escalation",
        ) ??
          copy(
            pageContract,
            "modal.rule_editor.default_vaccination_escalation",
          ))
      : copy(pageContract, "modal.rule_editor.default_vaccination_escalation");
  }
  function protocolPlaceholder(
    categoryKey: string,
    field: "code" | "name",
  ): string {
    const key = `${categoryKey}.${field}`;
    const value = protocolPlaceholders.find(
      (option) => option.key === key,
    )?.label;
    if (!value)
      throw new Error(
        `Admin-web page contract ${pageContract.route_id} missing option protocol_placeholders.${key}`,
      );
    return value;
  }
  function newContractDose(seq: number): DoseRow {
    const isPrimary = seq === 1;
    return {
      doseCode: isPrimary
        ? copy(pageContract, "modal.rule_editor.default_dose_primary")
        : `${copy(pageContract, "modal.rule_editor.default_dose_prefix")}${seq}`,
      trigger: isPrimary
        ? requireKey(triggerOptions, "birth_age", "trigger_types")
        : requireKey(
            triggerOptions,
            "after_previous_completion",
            "trigger_types",
          ),
      offsetDays: isPrimary ? 28 : 49,
      dueWindowDays: 7,
      doseAmount: 2,
      doseUnit: requireKey(doseUnitOptions, "ml", "dose_units"),
      vialDoses: 0,
      revaccinationIntervalDays: 0,
      scheduleNote: "",
      routeSite: requireKey(routeSiteOptions, "subcutaneous", "route_sites"),
      maxDelayDays: 7,
      courseLapsePolicy: requireKey(
        courseLapseOptions,
        "pc_review",
        "course_lapse_policies",
      ),
      repeat: firstKey(repeatOptions, "repeat_policies"),
      repeatUntilAfterAge: copy(
        pageContract,
        "modal.rule_editor.default_repeat_until",
      ),
      minGapDays: isPrimary ? 0 : 21,
      catchUp: requireKey(catchUpOptions, "pc_approval", "catch_up_policies"),
      sopVersion: firstKeyOrEmpty(scheduleSopOptions),
      proofCsv: copy(pageContract, "modal.rule_editor.default_proof_policy"),
    };
  }
  function newContractFeedFields(): FeedFields {
    const fallback = newFeedFields();
    const quantity = Number(
      optionalCopy(pageContract, "modal.rule_editor.default_feed_quantity") ??
        fallback.quantity,
    );
    return {
      animalStage: firstKeyOr(animalStageScope, fallback.animalStage),
      breedClass: firstKeyOr(feedClassOptions, fallback.breedClass),
      sourceTables: feedSourceTableOptions.map((option) => option.key),
      parameterFamilies: feedParameterFamilyOptions.map((option) => option.key),
      dimensionKeys: feedDimensionOptions.map((option) => option.key),
      ratioPolicy: firstKeyOr(feedRatioOptions, fallback.ratioPolicy),
      feedItem: firstKeyOr(feedItemOptions, fallback.feedItem),
      quantity: Number.isFinite(quantity) ? quantity : fallback.quantity,
      unit: firstKeyOr(feedUnitOptions, fallback.unit),
      sessionTimes:
        optionalCopy(
          pageContract,
          "modal.rule_editor.placeholder.session_timing",
        ) ?? fallback.sessionTimes,
      slotWeights:
        optionalCopy(
          pageContract,
          "modal.rule_editor.placeholder.session_weights",
        ) ?? fallback.slotWeights,
      packingProofCsv:
        optionalCopy(
          pageContract,
          "modal.rule_editor.placeholder.packing_proof",
        ) ?? fallback.packingProofCsv,
      executionProofCsv:
        optionalCopy(
          pageContract,
          "modal.rule_editor.placeholder.execution_proof",
        ) ?? fallback.executionProofCsv,
      inventoryPolicy: firstKeyOr(
        feedInventoryOptions,
        fallback.inventoryPolicy,
      ),
      validationChecks: feedValidationOptions.map((option) => option.key),
      calculationOutputs: feedCalculationOptions.map((option) => option.key),
    };
  }

  // Stage bands come from the backend (animal_stage_lookup). Three states, kept distinct so an outage
  // never reads as "no config": (1) loaded + non-empty → normal picker; (2) loaded + empty → honest
  // seed-state (disabled-with-reason, all-stages draft still allowed); (3) READ FAILED (stagesError)
  // → we don't know the true stage set, so the picker is disabled and Save/Publish are blocked.
  const stagesSeeded = animalStages.length > 0;
  const categoriesSeeded = visibleRuleCategories.length > 0;
  const categoryBlockReason = categoriesSeeded
    ? ""
    : `${copy(pageContract, "modal.rule_editor.category_seed_block_prefix")} — ${copy(pageContract, "modal.rule_editor.categories_empty")}`;
  const scheduleSopLabelsSeeded = scheduleSopOptions.length > 0;
  const noStagesReason = copy(
    pageContract,
    "modal.rule_editor.no_stages_reason",
  );
  const noSopLabelsReason = copy(
    pageContract,
    "modal.rule_editor.no_sop_labels_reason",
  );
  const stagePickerDisabled = !stagesSeeded || !!stagesError;
  const stageBlockReason = stagesError
    ? `${copy(pageContract, "modal.rule_editor.stage_load_block_prefix")} (${stagesError}) — ${copy(pageContract, "modal.rule_editor.fix_reload_suffix")}`
    : "";
  // SOP versions follow the same three states. A FAILED read (sopsError) must not look like an empty
  // SOP Library: disable the picker and block Save/Publish until the read succeeds. A loaded-but-empty
  // list is the honest "no published SOP version" state (picker stays usable, publish gated on select).
  const sopReadFailed = !!sopsError;
  const sopBlockReason = sopsError
    ? `${copy(pageContract, "modal.rule_editor.sop_load_block_prefix")} (${sopsError}) — ${copy(pageContract, "modal.rule_editor.fix_reload_suffix")}`
    : "";
  const defaultCategory = categoryDefault();
  const [category, setCategory] = useState(defaultCategory);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [scope, setScope] = useState(firstKey(scopeOptions, "rule_scopes"));
  const [effectiveFrom, setEffectiveFrom] = useState("");
  const [datePickerOpen, setDatePickerOpen] = useState(false);
  const [infoPanel, setInfoPanel] = useState<InfoPanel | null>(null);
  const [sopVersionId, setSopVersionId] = useState("");

  // Default to the first backend stage band (lowest sort_order) when seeded, else the ALL_STAGES UI
  // filter. No hardcoded K1 default — the stage vocabulary is owned by animal_stage_lookup.
  function newMatrixRow(
    seq: number,
    seed?: Partial<VaccinationMatrixRow>,
  ): VaccinationMatrixRow {
    const id =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `matrix-row-${Date.now()}-${seq}`;
    return {
      id,
      enabled: seed?.enabled ?? true,
      vaccine: {
        code: seed?.vaccine?.code ?? "",
        name: seed?.vaccine?.name ?? "",
        type:
          seed?.vaccine?.type ?? firstKey(vaccineTypeOptions, "vaccine_types"),
        pathogenClass:
          seed?.vaccine?.pathogenClass ??
          firstKey(pathogenClassOptions, "vaccine_pathogen_classes"),
        courseType:
          seed?.vaccine?.courseType ??
          firstKey(courseTypeOptions, "vaccine_course_types"),
        inventoryItemId: seed?.vaccine?.inventoryItemId ?? "",
        manufacturer: seed?.vaccine?.manufacturer ?? "",
        disease: seed?.vaccine?.disease ?? "",
        compatibilityGroup: seed?.vaccine?.compatibilityGroup ?? "",
      },
      species: seed?.species ?? firstKey(speciesOptions, "rule_species"),
      stage:
        seed?.stage ??
        animalStages[seq - 1]?.code ??
        animalStages[0]?.code ??
        firstKey(animalStageScope, "animal_stage_scope"),
      sex: seed?.sex ?? firstKey(sexOptions, "rule_sexes"),
      breed: seed?.breed ?? firstKey(breedOptions, "rule_breeds"),
      doses: seed?.doses,
    };
  }
  const [matrixRows, setMatrixRows] = useState<VaccinationMatrixRow[]>(() => [
    newMatrixRow(1),
  ]);
  const [selectedMatrixRowId, setSelectedMatrixRowId] = useState<string | null>(
    null,
  );
  const selectedMatrixRow =
    matrixRows.find((row) => row.id === selectedMatrixRowId) ??
    matrixRows[0] ??
    newMatrixRow(1);
  const selectedMatrixRowIndex = Math.max(
    matrixRows.findIndex((row) => row.id === selectedMatrixRow.id),
    0,
  );
  const vaccineCode = selectedMatrixRow.vaccine.code;
  const vaccineName = selectedMatrixRow.vaccine.name;
  const vaccineType = selectedMatrixRow.vaccine.type;
  const vaccinePathogenClass = selectedMatrixRow.vaccine.pathogenClass;
  const vaccineCourseType = selectedMatrixRow.vaccine.courseType;
  const vaccineInventoryItemId = selectedMatrixRow.vaccine.inventoryItemId;
  const vaccineManufacturer = selectedMatrixRow.vaccine.manufacturer;
  const vaccineDisease = selectedMatrixRow.vaccine.disease;
  const vaccineCompatibilityGroup =
    selectedMatrixRow.vaccine.compatibilityGroup;
  const species = selectedMatrixRow.species;
  const stage = selectedMatrixRow.stage;
  const sex = selectedMatrixRow.sex;
  const breed = selectedMatrixRow.breed;
  const [health, setHealth] = useState(
    requireKey(healthOptions, "any", "rule_healths"),
  );
  const [lifecycle, setLifecycle] = useState(
    firstKey(lifecycleOptions, "rule_lifecycles"),
  );
  const [reproductive, setReproductive] = useState(
    firstKey(reproductiveOptions, "rule_reproductive"),
  );
  const [excludeReproductiveStates, setExcludeReproductiveStates] = useState<
    string[]
  >(() => excludedReproductiveOptions.map((option) => option.key));
  const [deferStates, setDeferStates] = useState<string[]>(() =>
    deferOptions.map((option) => option.key),
  );
  const [vaccineLotPolicy, setVaccineLotPolicy] = useState(
    copy(pageContract, "modal.rule_editor.default_vaccine_lot_policy"),
  );

  const [missedDosePolicy, setMissedDosePolicy] = useState(
    requireKey(missedDoseOptions, "pc_approval", "missed_dose_policies"),
  );
  const [escalation, setEscalation] = useState(() =>
    defaultEscalation(defaultCategory),
  );

  const [compatibilityPolicy, setCompatibilityPolicy] =
    useState<CompatibilityPolicy>(() => newCompatibilityPolicy());
  const [procurementPolicy, setProcurementPolicy] = useState<ProcurementPolicy>(
    () => newProcurementPolicy(),
  );
  const [pregnancyPolicy, setPregnancyPolicy] = useState<PregnancyPolicy>(() =>
    newPregnancyPolicy(),
  );
  const [doses] = useState<DoseRow[]>(() => [newContractDose(1)]);
  const [feed, setFeed] = useState<FeedFields>(() => newContractFeedFields());

  const [versionId, setVersionId] = useState("");
  const [versionIds, setVersionIds] = useState<string[]>([]);
  // savedSig is the input signature persisted by the last successful Save. The saved DRAFT version
  // carries sop_version_id + proof_policy as they were AT SAVE TIME; Publish acts on that stored
  // version, not the live form. So editing SOP/proof/schedule after saving makes the form
  // "dirty" — Publish must be re-gated until a fresh Save persists the new values, else the backend
  // rejects (e.g. missing sop_version_id) on a version that no longer matches the form.
  const [savedSig, setSavedSig] = useState("");
  const [notice, setNotice] = useState<ActionResult | null>(null);
  const [impact, setImpact] = useState<ImpactPreviewResult | null>(null);
  const [pending, startTransition] = useTransition();
  const isFeedDirection =
    category === "feed_direction" && supportsFeedDirection;
  const [animalFilter, setAnimalFilter] = useState("all");
  const firstParkScope =
    scopeOptions.find((item) => item.key.startsWith("park:"))?.key ?? "";
  const sourceVaccinePresets = sourceVaccinePresetOptions
    .map(sourcePresetFromOption)
    .filter((preset) => preset.approvedKidWeeks.length > 0);
  function effectiveProtocolCode(): string {
    return category === "vaccination"
      ? VACCINATION_MATRIX_CODE
      : code.trim();
  }

  function effectiveProtocolName(): string {
    if (category !== "vaccination") return name.trim();
    return (
      name.trim() ||
      copy(pageContract, "modal.rule_editor.guided.default_plan_name")
    );
  }

  function friendlyScopeLabel(option: AdminUiOption): string {
    const raw = option.label.replace(/^park:\s*/i, "").trim();
    if (option.key === "tenant")
      return copy(pageContract, "modal.rule_editor.guided.company_default_scope");
    return raw || option.key.replace(/^park:/, "");
  }

  function formatEffectiveFromLabel(value: string): string {
    if (!value) return copy(pageContract, "modal.rule_editor.guided.pick_effective_date");
    const [year, month, day] = value.split("-");
    if (!year || !month || !day) return value;
    return `${day}/${month}/${year}`;
  }

  function localDateValue(date: Date): string {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    return `${year}-${month}-${day}`;
  }

  function parseLocalDate(value: string): Date {
    const [year, month, day] = value.split("-").map(Number);
    if (
      Number.isFinite(year) &&
      Number.isFinite(month) &&
      Number.isFinite(day)
    ) {
      return new Date(year, month - 1, day);
    }
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    return today;
  }

  function calendarMonthLabel(): string {
    return parseLocalDate(effectiveFrom).toLocaleDateString("en-IN", {
      month: "long",
      year: "numeric",
    });
  }

  function calendarDateOptions(): Array<{
    value: string;
    label: string;
    muted: boolean;
    today: boolean;
  }> {
    const selected = parseLocalDate(effectiveFrom);
    const monthStart = new Date(selected.getFullYear(), selected.getMonth(), 1);
    const gridStart = new Date(monthStart);
    gridStart.setDate(monthStart.getDate() - monthStart.getDay());
    const todayValue = localDateValue(new Date());
    return Array.from({ length: 42 }, (_, index) => {
      const date = new Date(gridStart);
      date.setDate(gridStart.getDate() + index);
      const value = localDateValue(date);
      return {
        value,
        label: String(date.getDate()),
        muted: date.getMonth() !== selected.getMonth(),
        today: value === todayValue,
      };
    });
  }

  function showInfo(
    anchor: HTMLElement,
    titleKey: string,
    bodyKey: string,
  ) {
    const title = copy(pageContract, titleKey);
    const body = copy(pageContract, bodyKey);
    const rect = anchor.getBoundingClientRect();
    const maxLeft = Math.max(8, window.innerWidth - 330);
    const left = Math.max(8, Math.min(rect.left, maxLeft));
    const top = Math.min(rect.bottom + 8, window.innerHeight - 120);
    setInfoPanel((previous) =>
      previous?.title === title && previous.body === body
        ? null
        : { title, body, top, left },
    );
  }

  function infoButton(titleKey: string, bodyKey: string) {
    return (
      <button
        type="button"
        className="cfginfoicon"
        onClick={(event) => showInfo(event.currentTarget, titleKey, bodyKey)}
        aria-label={copy(pageContract, "modal.rule_editor.guided.info_label")}
        title={copy(pageContract, "modal.rule_editor.guided.info_label")}
      >
        <Info className="ic" />
        <span>{copy(pageContract, "modal.rule_editor.guided.info_label")}</span>
      </button>
    );
  }

  function csvValues(value: string): string[] {
    return value
      .split(",")
      .map((token) => token.trim())
      .filter(Boolean);
  }

  function setSelectedProofTokens(tokens: string[]) {
    setSelectedProofCsv(tokens.join(","));
  }

  function toggleSelectedProofToken(token: string) {
    const current = csvValues(proofCsvForSelectedRow());
    const next = current.includes(token)
      ? current.filter((item) => item !== token)
      : [...current, token];
    setSelectedProofTokens(next);
  }

  function toggleProcurementToken(
    field: "firstWave" | "goatSecondWave" | "sheepSecondWave",
    token: string,
  ) {
    const current = csvValues(String(procurementPolicy[field] ?? ""));
    const next = current.includes(token)
      ? current.filter((item) => item !== token)
      : [...current, token];
    setProcurementField({ [field]: next.join(",") });
  }

  function patchMatrixRow(rowId: string, patch: Partial<VaccinationMatrixRow>) {
    setMatrixRows((rows) =>
      rows.map((row) => {
        if (row.id !== rowId) return row;
        return {
          ...row,
          ...patch,
          vaccine: patch.vaccine
            ? { ...row.vaccine, ...patch.vaccine }
            : row.vaccine,
        };
      }),
    );
    setSelectedMatrixRowId(rowId);
  }
  function patchSelectedMatrixRow(patch: Partial<VaccinationMatrixRow>) {
    patchMatrixRow(selectedMatrixRow.id, patch);
  }
  function addMatrixRow() {
    const next = newMatrixRow(matrixRows.length + 1, {
      doses: [],
    });
    setMatrixRows((rows) => [...rows, next]);
    setSelectedMatrixRowId(next.id);
  }
  function copySelectedMatrixRow() {
    const baseCode = selectedMatrixRow.vaccine.code.trim();
    const baseName = selectedMatrixRow.vaccine.name.trim();
    const sourceDoses =
      selectedMatrixRow.doses !== undefined ? selectedMatrixRow.doses : doses;
    const next = newMatrixRow(matrixRows.length + 1, {
      vaccine: {
        ...selectedMatrixRow.vaccine,
        code: baseCode ? `${baseCode}_COPY` : "",
        name: baseName ? `${baseName} copy` : "",
      },
      species: selectedMatrixRow.species,
      sex: selectedMatrixRow.sex,
      breed: selectedMatrixRow.breed,
      stage: selectedMatrixRow.stage,
      doses: cloneDoseRows(sourceDoses),
    });
    setMatrixRows((rows) => [...rows, next]);
    setSelectedMatrixRowId(next.id);
  }
  function cloneDoseRows(rows: DoseRow[]): DoseRow[] {
    return rows.map((row) => ({ ...row }));
  }
  function applySourcePresetToSelectedRow() {
    const preset = findSourcePreset(
      selectedMatrixRow.vaccine.code || selectedMatrixRow.vaccine.name,
    );
    if (!preset) return;
    patchSelectedMatrixRow(
      rowFromSourcePreset(
        preset,
        selectedMatrixRow.id,
        sourceMatrixStage(),
        preset.species || selectedMatrixRow.species,
        selectedMatrixRow.sex,
        selectedMatrixRow.breed,
      ),
    );
  }
  function loadHerdSourceMatrix() {
    const fallbackStage = sourceMatrixStage();
    const fallbackSpecies = firstKey(speciesOptions, "rule_species");
    const fallbackSex = firstKey(sexOptions, "rule_sexes");
    const fallbackBreed = firstKey(breedOptions, "rule_breeds");
    const rows = sourceVaccinePresets.map((preset, index) =>
      rowFromSourcePreset(
        preset,
        `source-vaccine-${index + 1}`,
        fallbackStage,
        preset.species || fallbackSpecies,
        fallbackSex,
        fallbackBreed,
      ),
    );
    const spacedRows = applyV1CompatibilitySpacing(rows);
    setMatrixRows(spacedRows);
    setSelectedMatrixRowId(spacedRows[0]?.id ?? null);
  }
  function matrixRowHasContent(row: VaccinationMatrixRow): boolean {
    return Boolean(row.vaccine.code.trim() || row.vaccine.name.trim());
  }
  function matrixRowIsActive(row: VaccinationMatrixRow): boolean {
    return row.enabled !== false && matrixRowHasContent(row);
  }
  const populatedMatrixRows = matrixRows.filter(matrixRowHasContent);
  const activeMatrixRows = matrixRows.filter(matrixRowIsActive);
  function rowMatchesAnimalScopeValue(
    row: VaccinationMatrixRow,
    scopeValue: string,
  ): boolean {
    if (scopeValue === "all") return true;
    const rowSpecies = (row.species || "all").toLowerCase();
    return (
      rowSpecies === scopeValue ||
      rowSpecies === "all" ||
      rowSpecies === "any"
    );
  }
  function speciesForAnimalScope(row: VaccinationMatrixRow): string {
    if (animalFilter === "all") return row.species;
    const rowSpecies = (row.species || "all").toLowerCase();
    return rowSpecies === "all" || rowSpecies === "any"
      ? animalFilter
      : row.species;
  }
  function rowForAnimalScope(row: VaccinationMatrixRow): VaccinationMatrixRow {
    const scopedSpecies = speciesForAnimalScope(row);
    return scopedSpecies === row.species ? row : { ...row, species: scopedSpecies };
  }
  const activeScopedMatrixRows = activeMatrixRows
    .filter((row) => rowMatchesAnimalScopeValue(row, animalFilter))
    .map(rowForAnimalScope);
  function setAnimalScope(next: string) {
    setAnimalFilter(next);
    const nextVisibleRows = populatedMatrixRows.filter((row) =>
      rowMatchesAnimalScopeValue(row, next),
    );
    if (!nextVisibleRows.some((row) => row.id === selectedMatrixRowId)) {
      setSelectedMatrixRowId(nextVisibleRows[0]?.id ?? null);
    }
  }
  function toggleMatrixRowEnabled(rowId: string) {
    const row = matrixRows.find((candidate) => candidate.id === rowId);
    patchMatrixRow(rowId, { enabled: !(row?.enabled !== false) });
  }
  function setSelectedProofCsv(value: string) {
    const rows = selectedDoses().map((dose) => ({ ...dose, proofCsv: value }));
    patchSelectedMatrixRow({ doses: rows });
  }
  function proofCsvForSelectedRow(): string {
    return selectedDoses()[0]?.proofCsv ?? "";
  }
  function proofTokensForMatrix(): string[] {
    return Array.from(
      new Set(
        activeScopedMatrixRows.flatMap((row) =>
          (row.doses ?? doses).flatMap((dose) =>
            dose.proofCsv
              .split(",")
              .map((token) => token.trim())
              .filter(Boolean),
          ),
        ),
      ),
    );
  }
  function rowDisplayName(row: VaccinationMatrixRow): string {
    return row.vaccine.name.trim() || row.vaccine.code.trim() || "matrix row";
  }
  function rowComboLabel(row: VaccinationMatrixRow): string {
    return [
      rowDisplayName(row),
      labelFromOptions(speciesOptions, row.species),
      stageLabel(row.stage),
      labelFromOptions(sexOptions, row.sex),
      labelFromOptions(breedOptions, row.breed),
    ].join(" / ");
  }
  function rowDisplayCode(row: VaccinationMatrixRow): string {
    return row.vaccine.code.trim() || rowDisplayName(row);
  }
  function labelFromOptions(options: AdminUiOption[], key: string): string {
    return options.find((option) => option.key === key)?.label ?? key;
  }
  function proofTokenLabel(token: string): string {
    return labelFromOptions(proofTokenOptions, token);
  }
  function stageLabel(stageCode: string): string {
    if (stageCode === ALL_STAGES_VALUE)
      return optionLabel(pageContract, "animal_stage_scope", ALL_STAGES_VALUE);
    return animalStages.find((item) => item.code === stageCode)?.label ?? stageCode;
  }
  function healthLabel(healthCode: string): string {
    return labelFromOptions(healthOptions, healthCode);
  }
  function selectedScopeLabel(): string {
    const selected = scopeOptions.find((item) => item.key === scope);
    return selected ? friendlyScopeLabel(selected) : scope;
  }
  function impactScopeSummary(): string {
    const stockLabel = vaccineInventoryItemId.trim()
      ? copy(pageContract, "modal.rule_editor.impact_stock_set")
      : copy(pageContract, "modal.rule_editor.impact_stock_missing");
    return [
      rowComboLabel(selectedMatrixRow),
      selectedScopeLabel(),
      healthLabel(health),
      `${selectedDoses().length} ${copy(pageContract, "modal.rule_editor.guided.dose_rows_count")}`,
      stockLabel,
    ].join(" · ");
  }
  function impactExplanation() {
    return (
      <div className="cfgimpact-explain">
        <div className="cfgimpact-scope">
          <b>{copy(pageContract, "modal.rule_editor.impact_scope_label")}</b>
          <span>{impactScopeSummary()}</span>
        </div>
        <div>
          <b>{copy(pageContract, "modal.rule_editor.impact_method_title")}</b>
          <p>{copy(pageContract, "modal.rule_editor.impact_method_body")}</p>
        </div>
        <p>{copy(pageContract, "modal.rule_editor.impact_scale_note")}</p>
      </div>
    );
  }
  function selectedRowPreset(): SourceVaccinePreset | undefined {
    return findSourcePreset(selectedMatrixRow.vaccine.code || selectedMatrixRow.vaccine.name);
  }
  function selectedRowPriority(): string {
    const priority = selectedRowPreset()?.priority;
    return priority ? String(priority) : "-";
  }
  function doseRoleLabel(dose: DoseRow, index: number, peerDoses: DoseRow[]): string {
    if (dose.trigger === "after_previous_completion" || dose.repeat === "every_n_days")
      return "repeat";
    if (peerDoses.length <= 1) return "single";
    if (index === 0) return "primary";
    if (index === 1) return "booster";
    return `dose ${index + 1}`;
  }
  function doseAgeText(dose: DoseRow, index = 0, peerDoses: DoseRow[] = []): string {
    const related = peerDoses.length > 0 ? peerDoses : [dose];
    const label = doseRoleLabel(dose, index, related);
    if (dose.trigger === "after_previous_completion" || dose.repeat === "every_n_days") {
      return `${label}: ${formatRevaccination(Number(dose.revaccinationIntervalDays) || Number(dose.offsetDays))}`;
    }
    const weeks = Number(dose.offsetDays) / 7;
    const when = Number.isInteger(weeks)
      ? `${weeks}w`
      : `${dose.offsetDays}d`;
    return `${label}: ${when} ±${dose.dueWindowDays}d`;
  }
  function rowSummary(row: VaccinationMatrixRow): string {
    const rowDoses = row.doses ?? doses;
    const kidDoses = rowDoses.filter((dose) => dose.trigger === "birth_age");
    const adultDose = rowDoses.find(
      (dose) => Number(dose.revaccinationIntervalDays) > 0 || dose.repeat === "every_n_days",
    );
    const kidText =
      kidDoses.length > 0
        ? kidDoses.map(doseAgeText).join(" · ")
        : "review timing";
    const adultText = adultDose
      ? `repeat ${formatRevaccination(Number(adultDose.revaccinationIntervalDays) || Number(adultDose.offsetDays))}`
      : "no repeat";
    return `${kidText} · ${adultText}`;
  }
  function rowTone(row: VaccinationMatrixRow): "live" | "killed" | "toxoid" | "other" {
    const value = row.vaccine.type.toLowerCase();
    if (value === "live") return "live";
    if (value === "killed") return "killed";
    if (value === "toxoid") return "toxoid";
    return "other";
  }
  const displayPlanRows = populatedMatrixRows.filter(
    (row) => rowMatchesAnimalScopeValue(row, animalFilter),
  );
  function sourceMatrixStage(): string {
    return animalStageScope.some((option) => option.key === ALL_STAGES_VALUE)
      ? ALL_STAGES_VALUE
      : firstKey(animalStageScope, "animal_stage_scope");
  }
  function applyV1CompatibilitySpacing(
    rows: VaccinationMatrixRow[],
  ): VaccinationMatrixRow[] {
    const occupiedLiveDays: Array<{ offset: number; species: string }> = [];
    return rows.map((row) => {
      const firstDose = row.doses?.[0];
      if (!firstDose || row.vaccine.type !== "live") return row;
      let effectiveOffset = firstDose.offsetDays;
      while (
        occupiedLiveDays.some(
          (slot) =>
            slot.offset === effectiveOffset &&
            speciesScopesOverlap(slot.species, row.species),
        )
      ) {
        effectiveOffset += compatibilityPolicy.liveToLiveGapDays;
      }
      occupiedLiveDays.push({ offset: effectiveOffset, species: row.species });
      if (effectiveOffset === firstDose.offsetDays) return row;
      return {
        ...row,
        doses: row.doses?.map((dose, index) =>
          index === 0
            ? {
                ...dose,
                offsetDays: effectiveOffset,
                doseCode: `${slugSource(row.vaccine.code)}_${Math.round(effectiveOffset / 7)}w`,
                scheduleNote: `${dose.scheduleNote}; V1 live-live spacing effective due ${effectiveOffset}d`,
              }
            : dose,
        ),
      };
    });
  }
  function speciesScopesOverlap(left: string, right: string): boolean {
    const a = (left || "all").toLowerCase();
    const b = (right || "all").toLowerCase();
    return a === b || a === "all" || a === "any" || b === "all" || b === "any";
  }
  function rowFromSourcePreset(
    preset: SourceVaccinePreset,
    id: string,
    stageValue: string,
    speciesValue: string,
    sexValue: string,
    breedValue: string,
  ): VaccinationMatrixRow {
    return {
      id,
      vaccine: {
        code: preset.code,
        name: preset.name,
        type: optionKeyOrFallback(vaccineTypeOptions, preset.vaccineType),
        pathogenClass: optionKeyOrFallback(
          pathogenClassOptions,
          preset.pathogenClass,
        ),
        courseType: optionKeyOrFallback(courseTypeOptions, preset.courseType),
        inventoryItemId: "",
        manufacturer: "tracked-matrix",
        disease: preset.disease,
        compatibilityGroup: preset.compatibilityGroup,
      },
      species: speciesValue,
      stage: stageValue,
      sex: sexValue,
      breed: breedValue,
      doses: dosesFromSourcePreset(preset),
    };
  }
  function dosesFromSourcePreset(preset: SourceVaccinePreset): DoseRow[] {
    const kidDoses = preset.approvedKidWeeks.map((week, index) => {
      const previousWeek = preset.approvedKidWeeks[index - 1] ?? 0;
      const gapDays = index === 0 ? 0 : (week - previousWeek) * 7;
      return {
        ...newContractDose(index + 1),
        doseCode: `${slugSource(preset.code)}_${week}w`,
        trigger: requireKey(triggerOptions, "birth_age", "trigger_types"),
        offsetDays: week * 7,
        dueWindowDays: 7,
        doseAmount: preset.doseAmount,
        doseUnit: requireKey(doseUnitOptions, "ml", "dose_units"),
        vialDoses: preset.vialDoses,
        revaccinationIntervalDays: preset.revaccinationDays,
        scheduleNote: scheduleNoteSummary(preset),
        minGapDays:
          index === 0
            ? 0
            : Math.max(gapDays, compatibilityPolicy.kidBoosterMinGapDays),
        repeat: "none",
        repeatUntilAfterAge: "-",
        catchUp: requireKey(
          catchUpOptions,
          "pc_approval",
          "catch_up_policies",
        ),
      };
    });
    return [...kidDoses, adultRevaccinationDose(preset, kidDoses.length + 1)];
  }
  function adultRevaccinationDose(
    preset: SourceVaccinePreset,
    seq: number,
  ): DoseRow {
    return {
      ...newContractDose(seq),
      doseCode: `${slugSource(preset.code)}_adult_revac_${preset.revaccinationDays}d`,
      trigger: requireKey(
        triggerOptions,
        "after_previous_completion",
        "trigger_types",
      ),
      offsetDays: preset.revaccinationDays,
      dueWindowDays: 30,
      maxDelayDays: 30,
      doseAmount: preset.doseAmount,
      doseUnit: requireKey(doseUnitOptions, "ml", "dose_units"),
      vialDoses: preset.vialDoses,
      revaccinationIntervalDays: preset.revaccinationDays,
      scheduleNote: `adult revaccination: ${formatRevaccination(preset.revaccinationDays)} after accepted completion; matrix revaccination ${formatRevaccination(preset.revaccinationDays)}`,
      minGapDays: preset.revaccinationDays,
      repeat: requireKey(repeatOptions, "every_n_days", "repeat_policies"),
      repeatUntilAfterAge: "lifetime",
      catchUp: requireKey(catchUpOptions, "next_cycle", "catch_up_policies"),
    };
  }
  function scheduleNoteSummary(preset: SourceVaccinePreset): string {
    return `kid critical schedule: approved timing ${formatWeeks(preset.approvedKidWeeks)} (${preset.approvedKidWeeks.map((week) => `${week * 7}d`).join(", ")} from DOB); adult revaccination: ${formatRevaccination(preset.revaccinationDays)} after accepted completion`;
  }
  function formatWeeks(weeks: number[]): string {
    if (weeks.length === 0) return "-";
    if (weeks.length === 1) return `${weeks[0]} weeks`;
    return `${weeks.slice(0, -1).join(", ")} and ${weeks[weeks.length - 1]} weeks`;
  }
  function formatRevaccination(days: number): string {
    switch (days) {
      case 182:
        return "6 months";
      case 274:
        return "9 months";
      case 365:
        return "1 year";
      case 1095:
        return "3 years";
      default:
        return `${days} days`;
    }
  }
  function findSourcePreset(value: string): SourceVaccinePreset | undefined {
    const key = slugSource(value);
    return sourceVaccinePresets.find(
      (preset) =>
        slugSource(preset.code) === key || slugSource(preset.name) === key,
    );
  }
  function sourcePresetFromOption(option: AdminUiOption): SourceVaccinePreset {
    const meta = parsePresetMeta(option.title);
    return {
      code: option.key,
      name: option.label,
      vaccineType: meta.vaccine_type ?? "unknown_review_needed",
      pathogenClass: meta.pathogen_class ?? "unknown_review_needed",
      courseType: meta.course_type ?? "single",
      disease: meta.disease ?? option.label,
      compatibilityGroup: meta.compatibility_group ?? option.key,
      approvedKidWeeks: numberList(meta.weeks),
      revaccinationDays: numberValue(meta.revaccination_days),
      doseAmount: numberValue(meta.dose_amount),
      vialDoses: numberValue(meta.vial_doses),
      priority: meta.priority ? numberValue(meta.priority) : undefined,
      species: meta.species ?? "all",
    };
  }
  function parsePresetMeta(raw: string): Record<string, string> {
    return raw.split("|").reduce<Record<string, string>>((acc, pair) => {
      const [key, ...rest] = pair.split("=");
      const value = rest.join("=");
      if (key && value) acc[key.trim()] = value.trim();
      return acc;
    }, {});
  }
  function numberList(raw: string | undefined): number[] {
    return (raw ?? "")
      .split(",")
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isFinite(value) && value > 0);
  }
  function numberValue(raw: string | undefined): number {
    const value = Number(raw ?? 0);
    return Number.isFinite(value) ? value : 0;
  }
  function optionKeyOrFallback(
    options: AdminUiOption[],
    desired: string,
  ): string {
    return options.some((option) => option.key === desired)
      ? desired
      : firstKey(options, "source_option");
  }
  function slugSource(value: string): string {
    return value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "");
  }
  function removeMatrixRow(rowId: string) {
    const next = matrixRows.filter((row) => row.id !== rowId);
    if (next.length === 0) return;
    setMatrixRows(next);
    if (selectedMatrixRowId === rowId)
      setSelectedMatrixRowId(next[0]?.id ?? null);
  }
  function setVaccineType(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, type: value },
    });
  }
  function setVaccinePathogenClass(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, pathogenClass: value },
    });
  }
  function setVaccineCourseType(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, courseType: value },
    });
  }
  function setVaccineInventoryItemId(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, inventoryItemId: value },
    });
  }
  function setVaccineManufacturer(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, manufacturer: value },
    });
  }
  function setVaccineDisease(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, disease: value },
    });
  }
  function setVaccineCompatibilityGroup(value: string) {
    patchSelectedMatrixRow({
      vaccine: { ...selectedMatrixRow.vaccine, compatibilityGroup: value },
    });
  }

  const input: RuleInput = useMemo(
    () => ({
      category,
      code: effectiveProtocolCode(),
      name: effectiveProtocolName(),
      scope,
      effectiveFrom,
      sopVersionId,
      vaccine: {
        code: vaccineCode,
        name: vaccineName,
        type: vaccineType,
        pathogenClass: vaccinePathogenClass,
        courseType: vaccineCourseType,
        inventoryItemId: vaccineInventoryItemId,
        manufacturer: vaccineManufacturer,
        disease: vaccineDisease,
        compatibilityGroup: vaccineCompatibilityGroup,
      },
      eligibility: {
        species,
        stage,
        sex,
        breed,
        lifecycle,
        health,
        reproductive,
        excludeReproductiveStates,
        deferStates,
      },
      vaccineLotPolicy,
      missedDosePolicy,
      escalation,
      compatibilityPolicy,
      procurementPolicy,
      pregnancyPolicy,
      doses,
      feed,
    }),
    [
      category,
      code,
      name,
      scope,
      effectiveFrom,
      sopVersionId,
      vaccineCode,
      vaccineName,
      vaccineType,
      vaccinePathogenClass,
      vaccineCourseType,
      vaccineInventoryItemId,
      vaccineManufacturer,
      vaccineDisease,
      vaccineCompatibilityGroup,
      species,
      stage,
      sex,
      breed,
      lifecycle,
      health,
      reproductive,
      excludeReproductiveStates,
      deferStates,
      vaccineLotPolicy,
      missedDosePolicy,
      escalation,
      compatibilityPolicy,
      procurementPolicy,
      pregnancyPolicy,
      doses,
      feed,
    ],
  );

  const dsl =
    category === "vaccination"
      ? buildVaccinationMatrixPreview(input, activeScopedMatrixRows)
      : buildRuleDsl(input);
  const inputSig = useMemo(
    () => JSON.stringify({ input, matrixRows, animalFilter }),
    [input, matrixRows, animalFilter],
  );
  // dirty = saved once, but the form has changed since — the stored version is stale for publish.
  const dirty = versionId !== "" && inputSig !== savedSig;
  const proofOk =
    category === "vaccination"
      ? proofTokensForMatrix().length > 0
      : hasProofRequirement(input);
  // The Publish gate, in priority order, so the title explains the first blocking reason. A failed
  // stage-reference read blocks first: we cannot trust eligibility authoring if the stage set is unknown.
  const publishBlock = categoryBlockReason
    ? categoryBlockReason
    : stageBlockReason
      ? stageBlockReason
      : sopBlockReason
        ? sopBlockReason
        : !canPublish
          ? publishDisabledReason ||
            copy(pageContract, "modal.rule_editor.only_ceo_publish")
          : !versionId
            ? copy(pageContract, "modal.rule_editor.save_first")
            : dirty
              ? copy(pageContract, "modal.rule_editor.dirty_publish")
              : !sopVersionId
                ? copy(pageContract, "modal.rule_editor.select_sop_publish")
                : !proofOk
                  ? copy(pageContract, "modal.rule_editor.proof_publish")
                  : "";
  const publishDisabled = pending || publishBlock !== "";

  function selectedDoses(): DoseRow[] {
    return selectedMatrixRow.doses !== undefined
      ? selectedMatrixRow.doses
      : doses;
  }
  function scheduleNoteDisplay(row: VaccinationMatrixRow): string {
    const scheduleNote = row.doses
      ?.find((dose) => dose.scheduleNote.trim())
      ?.scheduleNote.trim();
    return (
      scheduleNote ||
      copy(pageContract, "modal.rule_editor.table.no_schedule_note")
    );
  }
  function setSelectedDose(i: number, patch: Partial<DoseRow>) {
    const rows = selectedDoses().map((r, idx) =>
      idx === i ? { ...r, ...patch } : r,
    );
    patchSelectedMatrixRow({ doses: rows });
  }
  function addSelectedDose() {
    const rows = selectedDoses();
    patchSelectedMatrixRow({
      doses: [...rows, newContractDose(rows.length + 1)],
    });
  }
  function removeSelectedDose(i: number) {
    const rows = selectedDoses();
    if (rows.length <= 1) return;
    patchSelectedMatrixRow({ doses: rows.filter((_, idx) => idx !== i) });
  }
  function toggleDefer(value: string) {
    setDeferStates((prev) =>
      prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value],
    );
  }
  function toggleExcludeReproductive(value: string) {
    setExcludeReproductiveStates((prev) =>
      prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value],
    );
  }
  function changeCategory(nextCategory: string) {
    setCategory(nextCategory);
    setEscalation(defaultEscalation(nextCategory));
  }
  function setFeedField(patch: Partial<FeedFields>) {
    setFeed((prev) => ({ ...prev, ...patch }));
  }
  function setCompatibilityField(patch: Partial<CompatibilityPolicy>) {
    setCompatibilityPolicy((prev) => ({ ...prev, ...patch }));
  }
  function setProcurementField(patch: Partial<ProcurementPolicy>) {
    setProcurementPolicy((prev) => ({ ...prev, ...patch }));
  }
  function setPregnancyField(patch: Partial<PregnancyPolicy>) {
    setPregnancyPolicy((prev) => ({ ...prev, ...patch }));
  }
  function toggleFeedArray(
    field:
      | "sourceTables"
      | "parameterFamilies"
      | "dimensionKeys"
      | "validationChecks"
      | "calculationOutputs",
    value: string,
  ) {
    setFeed((prev) => {
      const current = prev[field];
      return {
        ...prev,
        [field]: current.includes(value)
          ? current.filter((item) => item !== value)
          : [...current, value],
      };
    });
  }

  function preview() {
    if (category !== "vaccination") return;
    startTransition(async () => {
      const scheduleRows = selectedDoses().length;
      const parsedScope = parseScope(scope);
      const vaccineItemId = vaccineInventoryItemId.trim();
      const res = await runImpactPreview({
        species,
        stage,
        sex,
        breed,
        health,
        park_id:
          parsedScope.type === "park" && parsedScope.id
            ? parsedScope.id
            : undefined,
        vaccine_item_id: vaccineItemId || undefined,
        doses_per_goat: scheduleRows,
        dose_rows: scheduleRows,
        horizon_days: 30,
      });
      if (res.ok && res.data) setImpact(res.data);
      else
        setNotice({
          ok: false,
          message:
            res.message ??
            copy(pageContract, "modal.rule_editor.message.preview_failed"),
        });
    });
  }

  function save() {
    const readBlock = categoryBlockReason || stageBlockReason || sopBlockReason;
    if (readBlock) {
      setNotice({ ok: false, message: readBlock });
      return;
    }
    if (category === "vaccination" && activeScopedMatrixRows.length === 0) {
      setNotice({
        ok: false,
        message: copy(
          pageContract,
          "modal.rule_editor.guided.no_active_vaccine",
        ),
      });
      return;
    }
    startTransition(async () => {
      const res = await saveDraftBatch(
        input,
        category === "vaccination" ? activeScopedMatrixRows : matrixRows,
      );
      setNotice(res);
      if (res.ok && res.versionId) {
        setVersionId(res.versionId);
        setVersionIds(res.versionIds ?? [res.versionId]);
        setSavedSig(inputSig); // snapshot what was persisted, so Publish knows the form is clean
      }
    });
  }

  function publish() {
    startTransition(async () =>
      setNotice(
        await publishVersions(versionIds.length > 0 ? versionIds : [versionId]),
      ),
    );
  }

  if (!open) return null;

  if (category === "vaccination" && !isFeedDirection) {
    const selectedHasContent = matrixRowHasContent(selectedMatrixRow);
    const selectedRowDoses = selectedDoses();
    const selectedAgeDoses = selectedRowDoses.filter(
      (dose) => dose.trigger === "birth_age",
    );
    const selectedAdultDoses = selectedRowDoses.filter(
      (dose) => dose.trigger !== "birth_age",
    );
    const activeProofTokens = proofTokensForMatrix();
    return (
      <>
        {presentation === "modal" ? (
          <div className="cfgback on" onClick={onClose} />
        ) : null}
        <div
          className={
            presentation === "page" ? "cfgpage cfgpage-rule on" : "cfgmodal on"
          }
          data-testid="rule-editor"
          style={
            presentation === "modal"
              ? { width: "min(1180px,96vw)" }
              : undefined
          }
          role={presentation === "modal" ? "dialog" : "region"}
          aria-modal={presentation === "modal" ? true : undefined}
          aria-label={copy(pageContract, "modal.rule_editor.aria")}
        >
          <div className="cmh">
            <span
              className="fic"
              style={{
                background: "var(--brand-soft)",
                color: "var(--brand)",
                width: 32,
                height: 32,
                borderRadius: 9,
              }}
            >
              <Syringe className="ic" />
            </span>
            <div>
              <div className="b700">
                {copy(pageContract, "modal.rule_editor.guided_title")}
              </div>
              <div className="muted small">
                {copy(pageContract, "modal.rule_editor.guided_subtitle")}
              </div>
            </div>
            <div className="sp" style={{ flex: 1 }} />
            <button
              type="button"
              className="x"
              onClick={onClose}
              aria-label={copy(pageContract, "modal.rule_editor.close")}
            >
              <X className="ic" />
            </button>
          </div>

          <div className="cmb">
            <div className="cfgform" style={{ display: "grid", gap: 14 }}>
              {stagesError ? (
                <div className="alert warn" role="alert">
                  <AlertTriangle className="ic" />
                  <div>
                    {copy(pageContract, "modal.rule_editor.stages_error_prefix")}{" "}
                    ({stagesError}).{" "}
                    {copy(pageContract, "modal.rule_editor.stages_error_body")}
                  </div>
                </div>
              ) : null}
              {sopsError ? (
                <div className="alert warn" role="alert">
                  <AlertTriangle className="ic" />
                  <div>
                    {copy(pageContract, "modal.rule_editor.sops_error_prefix")} (
                    {sopsError}).{" "}
                    {copy(pageContract, "modal.rule_editor.sops_error_body")}
                  </div>
                </div>
              ) : null}
              {categoryBlockReason ? (
                <div className="alert warn" role="alert">
                  <AlertTriangle className="ic" />
                  <div>{categoryBlockReason}</div>
                </div>
              ) : null}
              {notice ? (
                notice.ok ? (
                  <div className="note">
                    <span className="tag t-ok">
                      {copy(pageContract, "modal.rule_editor.notice_ok")}
                    </span>{" "}
                    {notice.message}
                  </div>
                ) : (
                  <div className="alert warn">
                    <AlertTriangle className="ic" />
                    <div>{notice.message}</div>
                  </div>
                )
              ) : null}
              {infoPanel ? (
                <div
                  className="cfginfo-popover"
                  role="dialog"
                  aria-label={infoPanel.title}
                  style={{ top: infoPanel.top, left: infoPanel.left }}
                >
                  <Info className="ic" />
                  <div>
                    <b>{infoPanel.title}</b>
                    <div className="muted small" style={{ marginTop: 4 }}>
                      {infoPanel.body}
                    </div>
                  </div>
                  <div className="sp" />
                  <button
                    type="button"
                    className="cfginfo-close"
                    onClick={() => setInfoPanel(null)}
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.guided.close_information",
                    )}
                  >
                    <X className="ic" />
                  </button>
                </div>
              ) : null}

              <section className="card">
                <div className="hd">
                  <h3>{copy(pageContract, "modal.rule_editor.guided.who_when_title")}</h3>
                  <div className="sp" />
                  <span className={`tag ${publishBlock ? "t-warn" : "t-ok"}`}>
                    {versionId
                      ? copy(pageContract, "modal.rule_editor.guided.draft")
                      : copy(pageContract, "modal.rule_editor.guided.draft")}
                  </span>
                </div>
                <div className="bd" style={{ display: "grid", gap: 12 }}>
                    <div className="hd" style={{ padding: 0, borderBottom: 0 }}>
                      <label style={{ margin: 0 }}>
                        {copy(pageContract, "modal.rule_editor.guided.plan_name")}
                      </label>
                      {infoButton(
                        "modal.rule_editor.guided.one_active_matrix_title",
                        "modal.rule_editor.guided.one_active_matrix_body",
                      )}
                    </div>
                    <input
                      aria-label={copy(pageContract, "modal.rule_editor.guided.plan_name")}
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder={copy(
                        pageContract,
                        "modal.rule_editor.guided.default_plan_name",
                      )}
                    />

                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: "repeat(2,minmax(0,1fr))",
                      gap: 10,
                    }}
                  >
                    <div>
                      <label>
                        {copy(pageContract, "modal.rule_editor.guided.applies_to")}
                      </label>
                      <div className="cfgchk" role="group">
                        <button
                          type="button"
                          className="btn sm"
                          onClick={() => setScope("tenant")}
                          disabled={!scopeOptions.some((item) => item.key === "tenant")}
                          aria-pressed={scope === "tenant"}
                          style={
                            scope === "tenant"
                              ? { borderColor: "var(--brand)", color: "var(--brand)" }
                              : undefined
                          }
                        >
                          {copy(pageContract, "modal.rule_editor.guided.whole_company")}
                        </button>
                        <button
                          type="button"
                          className="btn sm"
                          onClick={() => firstParkScope && setScope(firstParkScope)}
                          disabled={!scopeOptions.some((item) => item.key.startsWith("park:"))}
                          aria-pressed={scope.startsWith("park:")}
                          style={
                            scope.startsWith("park:")
                              ? { borderColor: "var(--brand)", color: "var(--brand)" }
                              : undefined
                          }
                        >
                          {copy(pageContract, "modal.rule_editor.guided.one_park")}
                        </button>
                      </div>
                    </div>
                    <div>
                      <label>
                        {copy(pageContract, "modal.rule_editor.guided.animals")}
                      </label>
                      <div className="cfgchk" role="group">
                        {[
                          ["all", "modal.rule_editor.guided.goats_sheep"],
                          ["goat", "modal.rule_editor.guided.goats"],
                          ["sheep", "modal.rule_editor.guided.sheep"],
                        ].map(([key, copyKey]) => (
                          <button
                            key={key}
                            type="button"
                            className="btn sm"
                            onClick={() => setAnimalScope(key)}
                            aria-pressed={animalFilter === key}
                            style={
                              animalFilter === key
                                ? { borderColor: "var(--brand)", color: "var(--brand)" }
                                : undefined
                            }
                          >
                            {copy(pageContract, copyKey)}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>

                  <div className="hd" style={{ padding: 0, borderBottom: 0 }}>
                      <label style={{ margin: 0 }}>
                        {copy(pageContract, "modal.rule_editor.field.scope")}
                      </label>
                      {infoButton(
                        "modal.rule_editor.guided.company_park_title",
                        "modal.rule_editor.guided.company_park_body",
                      )}
                  </div>
                  <div className="rowf">
                    <select
                      aria-label={copy(pageContract, "modal.rule_editor.field.scope")}
                      value={scope}
                      onChange={(e) => setScope(e.target.value)}
                    >
                      {scopeOptions.map((s) => (
                        <option key={s.key} value={s.key}>
                          {friendlyScopeLabel(s)}
                        </option>
                      ))}
                    </select>
                    <div style={{ position: "relative" }}>
                      <button
                        type="button"
                        className="btn cfgdate-button"
                        onClick={() => setDatePickerOpen((open) => !open)}
                        aria-expanded={datePickerOpen}
                      >
                        <span className="cfgdate-label">
                          <CalendarDays className="ic" />
                          {formatEffectiveFromLabel(effectiveFrom)}
                        </span>
                        <ChevronRight
                          className="ic"
                          style={{ transform: datePickerOpen ? "rotate(90deg)" : undefined }}
                        />
                      </button>
                      {datePickerOpen ? (
                        <div
                          className="card cfgdate-popover"
                        >
                          <div className="cfgcalendar-head">
                            <b>{calendarMonthLabel()}</b>
                            <input
                              type="date"
                              aria-label={copy(
                                pageContract,
                                "modal.rule_editor.guided.pick_effective_date",
                              )}
                              value={effectiveFrom}
                              onChange={(event) => {
                                setEffectiveFrom(event.target.value);
                                setDatePickerOpen(false);
                              }}
                            />
                          </div>
                          <div className="cfgcalendar-grid" aria-label={copy(pageContract, "modal.rule_editor.guided.pick_effective_date")}>
                            {["S", "M", "T", "W", "T", "F", "S"].map((day, index) => (
                              <span key={`${day}-${index}`} className="cfgcalendar-dow">
                                {day}
                              </span>
                            ))}
                            {calendarDateOptions().map((option) => (
                              <button
                                key={option.value}
                                type="button"
                                className={[
                                  "cfgcalendar-day",
                                  option.muted ? "muted-day" : "",
                                  option.today ? "today" : "",
                                  effectiveFrom === option.value ? "on" : "",
                                ].filter(Boolean).join(" ")}
                                onClick={() => {
                                  setEffectiveFrom(option.value);
                                  setDatePickerOpen(false);
                                }}
                              >
                                {option.label}
                              </button>
                            ))}
                          </div>
                        </div>
                      ) : null}
                    </div>
                  </div>

                  <label>
                    {copy(pageContract, "modal.rule_editor.field.sop_version")}
                  </label>
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.sop_version",
                    )}
                    value={sopVersionId}
                    onChange={(e) => setSopVersionId(e.target.value)}
                    disabled={sopReadFailed || sopVersions.length === 0}
                    title={sopReadFailed ? sopBlockReason : undefined}
                  >
                    <option value="">
                      {sopReadFailed
                        ? copy(pageContract, "modal.rule_editor.option.sops_failed")
                        : sopVersions.length === 0
                          ? copy(pageContract, "modal.rule_editor.option.no_sop")
                          : copy(pageContract, "modal.rule_editor.option.select_sop")}
                    </option>
                    {sopVersions.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <div className={publishBlock ? "alert warn" : "note"}>
                    {publishBlock ? (
                      <AlertTriangle className="ic" />
                    ) : (
                      <CheckCircle2 className="ic" />
                    )}
                    <div>
                      <b>
                        {publishBlock
                          ? copy(pageContract, "modal.rule_editor.guided.not_publishable")
                          : copy(pageContract, "modal.rule_editor.guided.publishable")}
                      </b>{" "}
                      {publishBlock ||
                        copy(pageContract, "modal.rule_editor.guided.publish_hint")}
                    </div>
                  </div>
                </div>
              </section>

              <section className="card">
                <div className="hd">
                  <h3>{copy(pageContract, "modal.rule_editor.guided.plan_title")}</h3>
                  <span className="tag t-ok">
                    {activeScopedMatrixRows.length}{" "}
                    {copy(pageContract, "modal.rule_editor.guided.on")}
                  </span>
                  <div className="sp" />
                  {infoButton(
                    "modal.rule_editor.guided.recommended_plan_info_title",
                    "modal.rule_editor.guided.recommended_plan_info_body",
                  )}
                  <button
                    type="button"
                    className="btn sm"
                    onClick={loadHerdSourceMatrix}
                  >
                    <CalendarDays className="ic" />{" "}
                    {copy(pageContract, "modal.rule_editor.guided.load_plan")}
                  </button>
                </div>
                <div className="bd" style={{ display: "grid", gap: 10 }}>
                  <div className="muted small">
                    {copy(pageContract, "modal.rule_editor.guided.plan_hint")}
                  </div>
                  {displayPlanRows.length === 0 ? (
                    <div className="note">
                      <Info className="ic" />
                      <div>
                        <b>
                          {copy(pageContract, "modal.rule_editor.guided.empty_title")}
                        </b>
                        <br />
                        {copy(pageContract, "modal.rule_editor.guided.empty_body")}
                      </div>
                    </div>
                  ) : (
                    displayPlanRows.map((row) => {
                      const selected = row.id === selectedMatrixRow.id;
                      const enabled = row.enabled !== false;
                      const tone = rowTone(row);
                      return (
                        <button
                          key={row.id}
                          type="button"
                          className="card"
                          onClick={() => setSelectedMatrixRowId(row.id)}
                          style={{
                            textAlign: "left",
                            padding: 12,
                            display: "grid",
                            gridTemplateColumns: "auto minmax(0,1fr) auto",
                            gap: 12,
                            alignItems: "center",
                            boxShadow: "none",
                            borderColor: selected ? "var(--brand)" : "var(--line)",
                            background: selected
                              ? "rgba(120, 210, 72, 0.08)"
                              : "var(--bg)",
                          }}
                        >
                          <span
                            role="switch"
                            aria-checked={enabled}
                            tabIndex={0}
                            onClick={(event) => {
                              event.stopPropagation();
                              toggleMatrixRowEnabled(row.id);
                            }}
                            onKeyDown={(event) => {
                              if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                event.stopPropagation();
                                toggleMatrixRowEnabled(row.id);
                              }
                            }}
                            className={`tag ${enabled ? "t-ok" : "t-mut"}`}
                          >
                            {enabled
                              ? copy(pageContract, "modal.rule_editor.guided.included")
                              : copy(pageContract, "modal.rule_editor.guided.skipped")}
                          </span>
                          <span style={{ minWidth: 0 }}>
                            <span
                              style={{
                                display: "flex",
                                alignItems: "center",
                                gap: 8,
                                flexWrap: "wrap",
                              }}
                            >
                              <b>{rowDisplayCode(row)}</b>
                              <span className="tag t-info">
                                {labelFromOptions(speciesOptions, speciesForAnimalScope(row))}
                              </span>
                              <span className="tag t-mut">
                                {labelFromOptions(vaccineTypeOptions, row.vaccine.type)}
                              </span>
                              <span
                                style={{
                                  width: 8,
                                  height: 8,
                                  borderRadius: 999,
                                  background:
                                    tone === "live"
                                      ? "var(--warn)"
                                      : tone === "killed"
                                        ? "var(--info)"
                                        : "var(--brand)",
                                }}
                              />
                            </span>
                            <span className="muted small" style={{ display: "block", marginTop: 4 }}>
                              {rowSummary(row)}
                            </span>
                          </span>
                            <span className="btn sm" aria-hidden="true">
                              <Pencil className="ic" />{" "}
                              {copy(pageContract, "modal.rule_editor.guided.edit_timing")}
                            </span>
                        </button>
                      );
                    })
                  )}
                </div>
              </section>

              <section className="card">
                <div className="hd">
                  <h3>{copy(pageContract, "modal.rule_editor.guided.selected_timing")}</h3>
                  <div className="sp" />
                  {infoButton(
                    "modal.rule_editor.guided.who_qualifies_info_title",
                    "modal.rule_editor.guided.who_qualifies_info_body",
                  )}
                  {selectedHasContent ? (
                    <span className="tag t-info">{rowDisplayName(selectedMatrixRow)}</span>
                  ) : null}
                </div>
                <div className="bd" style={{ display: "grid", gap: 12 }}>
                  {!selectedHasContent ? (
                    <div className="note">
                      <Info className="ic" />
                      <div>
                        {copy(pageContract, "modal.rule_editor.guided.no_selected")}
                      </div>
                    </div>
                  ) : (
                    <>
                      <div className="rowf">
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.vaccine_code",
                          )}
                          value={selectedMatrixRow.vaccine.code}
                          onChange={(e) =>
                            patchSelectedMatrixRow({
                              vaccine: {
                                ...selectedMatrixRow.vaccine,
                                code: e.target.value,
                              },
                            })
                          }
                        />
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.vaccine_name",
                          )}
                          value={selectedMatrixRow.vaccine.name}
                          onChange={(e) =>
                            patchSelectedMatrixRow({
                              vaccine: {
                                ...selectedMatrixRow.vaccine,
                                name: e.target.value,
                              },
                            })
                          }
                        />
                      </div>
                      <div className="grid g4">
                        {[
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_type"),
                            labelFromOptions(vaccineTypeOptions, vaccineType),
                          ],
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_species"),
                            labelFromOptions(speciesOptions, species),
                          ],
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_dose"),
                            `${selectedRowDoses[0]?.doseAmount ?? "-"} ${selectedRowDoses[0]?.doseUnit ?? ""}`,
                          ],
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_vial"),
                            String(selectedRowDoses[0]?.vialDoses || "-"),
                          ],
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_revaccination"),
                            formatRevaccination(
                              Number(
                                selectedRowDoses.find(
                                  (dose) =>
                                    Number(dose.revaccinationIntervalDays) > 0,
                                )?.revaccinationIntervalDays ?? 0,
                              ),
                            ),
                          ],
                          [
                            copy(pageContract, "modal.rule_editor.guided.fact_priority"),
                            selectedRowPriority(),
                          ],
                        ].map(([label, value]) => (
                          <div key={label} className="kpi">
                            <div className="lab">{label}</div>
                            <div className="val" style={{ fontSize: 16 }}>
                              {value}
                            </div>
                          </div>
                        ))}
                      </div>

                      <label>
                        {copy(pageContract, "modal.rule_editor.guided.who_qualifies")}
                      </label>
                      <div className="rowf">
                        <select
                          aria-label={copy(pageContract, "modal.rule_editor.table.stage")}
                          value={stage}
                          onChange={(e) =>
                            patchSelectedMatrixRow({ stage: e.target.value })
                          }
                          disabled={stagePickerDisabled}
                          title={
                            stagesError
                              ? stageBlockReason
                              : stagesSeeded
                                ? undefined
                                : noStagesReason
                          }
                        >
                          <option value={ALL_STAGES_VALUE}>
                            {optionLabel(
                              pageContract,
                              "animal_stage_scope",
                              ALL_STAGES_VALUE,
                            )}
                          </option>
                          {animalStages.map((s) => (
                            <option key={s.code} value={s.code}>
                              {s.label}
                            </option>
                          ))}
                        </select>
                        <select
                          aria-label={copy(pageContract, "modal.rule_editor.table.species")}
                          value={species}
                          onChange={(e) =>
                            patchSelectedMatrixRow({ species: e.target.value })
                          }
                        >
                          {speciesOptions.map((s) => (
                            <option key={s.key} value={s.key}>
                              {s.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div className="rowf">
                        <select
                          aria-label={copy(pageContract, "modal.rule_editor.table.sex")}
                          value={sex}
                          onChange={(e) =>
                            patchSelectedMatrixRow({ sex: e.target.value })
                          }
                        >
                          {sexOptions.map((s) => (
                            <option key={s.key} value={s.key}>
                              {s.label}
                            </option>
                          ))}
                        </select>
                        <select
                          aria-label={copy(pageContract, "modal.rule_editor.table.breed")}
                          value={breed}
                          onChange={(e) =>
                            patchSelectedMatrixRow({ breed: e.target.value })
                          }
                        >
                          {breedOptions.map((s) => (
                            <option key={s.key} value={s.key}>
                              {s.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div className="muted small">
                        {copy(pageContract, "modal.rule_editor.guided.qualifies_hint")}{" "}
                        {stageLabel(stage)}
                      </div>

                      <details open>
                        <summary className="b700">
                          {copy(pageContract, "modal.rule_editor.guided.schedule_editor")}
                        </summary>
                        <div className="cfgdose-scope">
                          <span>{copy(pageContract, "modal.rule_editor.guided.selected_combo_label")}</span>
                          <b>{rowComboLabel(selectedMatrixRow)}</b>
                          <span className="tag t-info">
                            {selectedRowDoses.length}{" "}
                            {copy(pageContract, "modal.rule_editor.guided.dose_rows_count")}
                          </span>
                        </div>
                        <div className="muted small" style={{ margin: "6px 0 8px" }}>
                          {copy(pageContract, "modal.rule_editor.guided.schedule_hint")}
                        </div>
                        <div className="cfgtablewrap cfgschedule-table">
                          <table>
                            <thead>
                              <tr>
                                <th>{copy(pageContract, "modal.rule_editor.table.dose")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.trigger")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.offset")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.window")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.dose_amount")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.vial_doses")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.revaccination")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.max_delay")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.min_gap")}</th>
                                <th>{copy(pageContract, "modal.rule_editor.table.proof_policy")}</th>
                              </tr>
                            </thead>
                            <tbody>
                              {selectedRowDoses.map((d, i) => (
                                <tr key={i}>
                                  <td style={{ minWidth: 130 }}>
                                    <div className="note" style={{ padding: "7px 9px" }}>
                                      <div>
                                        <b>{doseRoleLabel(d, i, selectedRowDoses)}</b>
                                        <div className="muted small">{doseAgeText(d, i, selectedRowDoses)}</div>
                                      </div>
                                    </div>
                                  </td>
                                  <td style={{ minWidth: 150 }}>
                                    <select
                                      aria-label={copy(pageContract, "modal.rule_editor.table.trigger")}
                                      value={d.trigger}
                                      onChange={(e) =>
                                        setSelectedDose(i, { trigger: e.target.value })
                                      }
                                    >
                                      {triggerOptions.map((t) => (
                                        <option key={t.key} value={t.key}>
                                          {t.label}
                                        </option>
                                      ))}
                                    </select>
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.offset")}
                                      type="number"
                                      value={d.offsetDays}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          offsetDays: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.window")}
                                      type="number"
                                      value={d.dueWindowDays}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          dueWindowDays: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.dose_amount")}
                                      type="number"
                                      min="0"
                                      step="0.01"
                                      value={d.doseAmount}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          doseAmount: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.vial_doses")}
                                      type="number"
                                      min="0"
                                      value={d.vialDoses}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          vialDoses: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.revaccination")}
                                      type="number"
                                      min="0"
                                      value={d.revaccinationIntervalDays}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          revaccinationIntervalDays: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.max_delay")}
                                      type="number"
                                      value={d.maxDelayDays}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          maxDelayDays: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td>
                                    <input
                                      aria-label={copy(pageContract, "modal.rule_editor.table.min_gap")}
                                      type="number"
                                      value={d.minGapDays}
                                      onChange={(e) =>
                                        setSelectedDose(i, {
                                          minGapDays: Number(e.target.value),
                                        })
                                      }
                                    />
                                  </td>
                                  <td style={{ minWidth: 220 }}>
                                    <div className="cfgchk" aria-label={copy(pageContract, "modal.rule_editor.table.proof_policy")}>
                                      {csvValues(d.proofCsv).length > 0 ? (
                                        csvValues(d.proofCsv).map((token) => (
                                          <span key={token} className="tag t-info">
                                            {proofTokenLabel(token)}
                                          </span>
                                        ))
                                      ) : (
                                          <span className="muted small">
                                            {copy(
                                              pageContract,
                                              "modal.rule_editor.guided.set_below",
                                            )}
                                          </span>
                                      )}
                                    </div>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      </details>

                      <div className="rowf">
                        <div>
                          <label>
                            {copy(pageContract, "modal.rule_editor.guided.age_course")}
                          </label>
                          <div className="note">
                            {selectedAgeDoses.map(doseAgeText).join(" · ") ||
                              copy(pageContract, "label.placeholder")}
                          </div>
                        </div>
                        <div>
                          <label>
                            {copy(pageContract, "modal.rule_editor.guided.procurement_course")}
                          </label>
                          <div className="note">
                            {selectedAdultDoses.map(doseAgeText).join(" · ") ||
                              copy(pageContract, "label.placeholder")}
                          </div>
                        </div>
                      </div>
                    </>
                  )}
                </div>
              </section>

              <section className="card">
                <div className="hd">
                  <ShieldCheck className="ic" />
                  <h3>{copy(pageContract, "modal.rule_editor.guided.safety_title")}</h3>
                  <span className="tag t-info">
                    {copy(pageContract, "modal.rule_editor.guided.read_only")}
                  </span>
                </div>
                <div className="bd" style={{ display: "grid", gap: 8 }}>
                  <div className="muted small">
                    {copy(pageContract, "modal.rule_editor.guided.safety_hint")}
                  </div>
                  <div className="cfgsafety-list">
                    {[
                      "modal.rule_editor.guided.safety_max_shots",
                      "modal.rule_editor.guided.safety_live_live",
                      "modal.rule_editor.guided.safety_killed_live",
                      "modal.rule_editor.guided.safety_pregnancy",
                      "modal.rule_editor.guided.safety_defer",
                      "modal.rule_editor.guided.safety_mother",
                      "modal.rule_editor.guided.safety_batch",
                    ].map((key) => (
                      <div key={key} className="cfgsafety-row">
                        <CheckCircle2 className="ic" />
                        <span>{copy(pageContract, key)}</span>
                        <span className="tag t-mut">
                          {copy(pageContract, "modal.rule_editor.guided.read_only")}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              </section>

              <section className="card">
                <div className="hd">
                  <h3>{copy(pageContract, "modal.rule_editor.guided.procurement_title")}</h3>
                  <div className="sp" />
                  {infoButton(
                    "modal.rule_editor.guided.procurement_info_title",
                    "modal.rule_editor.guided.procurement_info_body",
                  )}
                </div>
                <div className="bd" style={{ display: "grid", gap: 8 }}>
                  <div className="muted small">
                    {copy(pageContract, "modal.rule_editor.guided.procurement_hint")}
                  </div>
                  <div>
                    <label>{copy(pageContract, "modal.rule_editor.field.first_wave")}</label>
                    <div className="cfgchk">
                      {procurementVaccineChoices.map((option) => {
                        const selected = csvValues(procurementPolicy.firstWave).includes(option.key);
                        return (
                          <button
                            key={option.key}
                            type="button"
                            className={`tag ${selected ? "t-ok" : "t-mut"}`}
                            onClick={() => toggleProcurementToken("firstWave", option.key)}
                          >
                            {option.label}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                  <div className="rowf">
                    <div>
                      <label className="cfglabel-with-info">
                        {copy(pageContract, "modal.rule_editor.field.second_wave_after_days")}
                        {infoButton(
                          "modal.rule_editor.guided.procurement_info_title",
                          "modal.rule_editor.guided.procurement_second_visit_note",
                        )}
                      </label>
                      <input
                        aria-label={copy(pageContract, "modal.rule_editor.field.second_wave_after_days")}
                        type="number"
                        min="0"
                        value={procurementPolicy.secondWaveAfterDays}
                        onChange={(e) =>
                          setProcurementField({
                            secondWaveAfterDays: Number(e.target.value),
                          })
                        }
                      />
                    </div>
                  </div>
                  <div className="rowf">
                    <div>
                      <label>{copy(pageContract, "modal.rule_editor.field.goat_second_wave")}</label>
                      <div className="cfgchk">
                        {procurementVaccineChoices.map((option) => {
                          const selected = csvValues(procurementPolicy.goatSecondWave).includes(option.key);
                          return (
                            <button
                              key={option.key}
                              type="button"
                              className={`tag ${selected ? "t-ok" : "t-mut"}`}
                              onClick={() => toggleProcurementToken("goatSecondWave", option.key)}
                            >
                              {option.label}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                    <div>
                      <label>{copy(pageContract, "modal.rule_editor.field.sheep_second_wave")}</label>
                      <div className="cfgchk">
                        {procurementVaccineChoices.map((option) => {
                          const selected = csvValues(procurementPolicy.sheepSecondWave).includes(option.key);
                          return (
                            <button
                              key={option.key}
                              type="button"
                              className={`tag ${selected ? "t-ok" : "t-mut"}`}
                              onClick={() => toggleProcurementToken("sheepSecondWave", option.key)}
                            >
                              {option.label}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <section className="card">
                <div className="hd">
                  <h3>{copy(pageContract, "modal.rule_editor.guided.proof_title")}</h3>
                  <div className="sp" />
                  {infoButton(
                    "modal.rule_editor.guided.proof_info_title",
                    "modal.rule_editor.guided.proof_info_body",
                  )}
                </div>
                <div className="bd" style={{ display: "grid", gap: 8 }}>
                  <div className="muted small">
                    {copy(pageContract, "modal.rule_editor.guided.proof_hint")}
                    </div>
                    <div className="cfgchk">
                      {proofTokenOptions.map((option) => {
                        const selected = csvValues(proofCsvForSelectedRow()).includes(option.key);
                        return (
                        <button
                          key={option.key}
                          type="button"
                          className={`tag ${selected ? "t-ok" : "t-mut"}`}
                          onClick={() => toggleSelectedProofToken(option.key)}
                        >
                          {option.label}
                        </button>
                      );
                    })}
                  </div>
                  <div className="cfgpolicy-note">
                    <span>{copy(pageContract, "modal.rule_editor.guided.vaccine_lot_policy_label")}</span>
                    <b>{vaccineLotPolicy}</b>
                  </div>
                    <div
                      className="cfgchk"
                      aria-label={copy(
                        pageContract,
                        "modal.rule_editor.guided.selected_proof_tokens_aria",
                      )}
                    >
                    {activeProofTokens.map((token) => (
                      <span key={token} className="tag t-info">
                        {proofTokenLabel(token)}
                      </span>
                    ))}
                  </div>
                </div>
              </section>

              <details className="card">
                <summary className="hd" style={{ cursor: "pointer" }}>
                  <h3>{copy(pageContract, "modal.rule_editor.guided.advanced_title")}</h3>
                </summary>
                <div className="bd">
                  <div className="muted small" style={{ marginBottom: 8 }}>
                    {copy(pageContract, "modal.rule_editor.guided.advanced_hint")}
                  </div>
                  <div
                    className="cfgjson"
                    aria-label={copy(pageContract, "modal.rule_editor.rule_dsl_aria")}
                  >
                    {JSON.stringify(dsl, null, 2)}
                  </div>
                </div>
              </details>
            </div>
          </div>

          <div className="cmh cfgsection-head cfgimpact-head">
            <h4 style={{ margin: 0 }}>
              {copy(pageContract, "modal.rule_editor.impact_title_vaccination")}
            </h4>
            <div className="sp" style={{ flex: 1 }} />
            <button
              type="button"
              className="btn sm"
              onClick={preview}
              disabled={pending}
            >
              {pending
                ? copy(pageContract, "action.computing")
                : copy(pageContract, "action.preview_impact")}
            </button>
          </div>
          <div className="cfgimpact-body">
            {impact ? (
              <>
                <div className="grid g4">
                  {[
                    [
                      copy(pageContract, "modal.rule_editor.kpi.eligible_goats"),
                      impact.eligible_goats,
                    ],
                    [
                      copy(pageContract, "modal.rule_editor.kpi.obligations"),
                      impact.obligations,
                    ],
                    [
                      copy(pageContract, "modal.rule_editor.kpi.batches"),
                      impact.batches,
                    ],
                    [
                      copy(pageContract, "modal.rule_editor.kpi.doses_required"),
                      impact.doses_required,
                    ],
                  ].map(([l, v]) => (
                    <div key={String(l)} className="kpi">
                      <div className="lab">{l}</div>
                      <div className="val">{String(v)}</div>
                    </div>
                  ))}
                </div>
                <div className="muted small" style={{ marginTop: 10 }}>
                  {copy(pageContract, "modal.rule_editor.label.doses_available")}:{" "}
                  <b>
                    {impact.doses_available ||
                      copy(pageContract, "label.placeholder")}
                  </b>
                  {impact.earliest_expiry
                    ? ` · ${copy(pageContract, "modal.rule_editor.label.earliest_expiry")} ${impact.earliest_expiry.slice(0, 10)}`
                    : ""}
                </div>
                {impact.warnings.length > 0
                  ? impact.warnings.map((w, i) => (
                      <div
                        key={i}
                        className="alert warn"
                        style={{ marginTop: 8 }}
                      >
                        <AlertTriangle className="ic" />
                        <div>{w}</div>
                      </div>
                    ))
                  : null}
                {impactExplanation()}
              </>
            ) : (
              <>
                <p className="muted small">
                  {copy(pageContract, "modal.rule_editor.preview_empty_vaccination")}
                </p>
                {impactExplanation()}
              </>
            )}
          </div>

          <div className="cfgmf">
            <button type="button" className="btn" onClick={onClose}>
              {copy(pageContract, "action.cancel")}
            </button>
            <div className="sp" style={{ flex: 1 }} />
            {versionId ? (
              <span className="muted small">
                {copy(pageContract, "modal.rule_editor.label.draft_saved")} ·{" "}
                {versionId.slice(0, 8)}
              </span>
            ) : null}
            <button
              type="button"
              className="btn"
              onClick={save}
              disabled={pending || !!stageBlockReason || !!sopBlockReason}
              title={stageBlockReason || sopBlockReason || undefined}
              style={
                stageBlockReason || sopBlockReason ? { opacity: 0.45 } : undefined
              }
            >
              {pending
                ? copy(pageContract, "action.saving")
                : copy(pageContract, "action.save_draft")}
            </button>
            <button
              type="button"
              className="btn p"
              onClick={publish}
              disabled={publishDisabled}
              title={publishBlock}
              style={publishDisabled ? { opacity: 0.45 } : undefined}
            >
              {canPublish
                ? copy(pageContract, "action.publish")
                : copy(pageContract, "action.publish_ceo")}
            </button>
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      {presentation === "modal" ? (
        <div className="cfgback on" onClick={onClose} />
      ) : null}
      <div
        className={
          presentation === "page" ? "cfgpage cfgpage-rule on" : "cfgmodal on"
        }
        data-testid="rule-editor"
        style={
          presentation === "modal" ? { width: "min(1180px,96vw)" } : undefined
        }
        role={presentation === "modal" ? "dialog" : "region"}
        aria-modal={presentation === "modal" ? true : undefined}
        aria-label={copy(pageContract, "modal.rule_editor.aria")}
      >
        <div className="cmh">
          <span
            className="fic"
            style={{
              background: "var(--brand-soft)",
              color: "var(--brand)",
              width: 32,
              height: 32,
              borderRadius: 9,
            }}
          >
            <Pencil className="ic" />
          </span>
          <div>
            <div className="b700">
              {copy(pageContract, "modal.rule_editor.title")}
            </div>
            <div className="muted small">
              {copy(pageContract, "modal.rule_editor.subtitle")}
            </div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button
            type="button"
            className="x"
            onClick={onClose}
            aria-label={copy(pageContract, "modal.rule_editor.close")}
          >
            <X className="ic" />
          </button>
        </div>

        <div className="cmb">
          <div className="cfgform">
            {stagesError ? (
              <div className="alert warn" role="alert">
                <AlertTriangle className="ic" />
                <div>
                  {copy(pageContract, "modal.rule_editor.stages_error_prefix")}{" "}
                  ({stagesError}).{" "}
                  {copy(pageContract, "modal.rule_editor.stages_error_body")}
                </div>
              </div>
            ) : null}
            {sopsError ? (
              <div className="alert warn" role="alert">
                <AlertTriangle className="ic" />
                <div>
                  {copy(pageContract, "modal.rule_editor.sops_error_prefix")} (
                  {sopsError}).{" "}
                  {copy(pageContract, "modal.rule_editor.sops_error_body")}
                </div>
              </div>
            ) : null}
            {categoryBlockReason ? (
              <div className="alert warn" role="alert">
                <AlertTriangle className="ic" />
                <div>{categoryBlockReason}</div>
              </div>
            ) : null}
            {notice ? (
              notice.ok ? (
                <div className="note">
                  <span className="tag t-ok">
                    {copy(pageContract, "modal.rule_editor.notice_ok")}
                  </span>{" "}
                  {notice.message}
                </div>
              ) : (
                <div className="alert warn">
                  <AlertTriangle className="ic" />
                  <div>{notice.message}</div>
                </div>
              )
            ) : null}

            <label>
              {copy(pageContract, "modal.rule_editor.field.category")}
            </label>
            <select
              aria-label={copy(
                pageContract,
                "modal.rule_editor.field.category",
              )}
              value={category}
              onChange={(e) => changeCategory(e.target.value)}
              disabled={!categoriesSeeded}
              title={categoryBlockReason || undefined}
            >
              {!categoriesSeeded ? (
                <option value={category}>
                  {copy(pageContract, "modal.rule_editor.option.no_categories")}
                </option>
              ) : (
                visibleRuleCategories.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.label}
                  </option>
                ))
              )}
            </select>

            <label>
              {copy(pageContract, "modal.rule_editor.field.protocol")}
            </label>
            <div className="rowf">
              <input
                aria-label={copy(
                  pageContract,
                  "modal.rule_editor.field.protocol",
                )}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={protocolPlaceholder(category, "code")}
              />
              <input
                aria-label={copy(
                  pageContract,
                  "modal.rule_editor.field.protocol",
                )}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={protocolPlaceholder(category, "name")}
              />
            </div>

            <label>{copy(pageContract, "modal.rule_editor.field.scope")}</label>
            <div className="rowf">
              <select
                aria-label={copy(pageContract, "modal.rule_editor.field.scope")}
                value={scope}
                onChange={(e) => setScope(e.target.value)}
              >
                {scopeOptions.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </select>
              <input
                type="date"
                aria-label={copy(pageContract, "modal.rule_editor.field.scope")}
                value={effectiveFrom}
                onChange={(e) => setEffectiveFrom(e.target.value)}
              />
            </div>

            <label>
              {copy(pageContract, "modal.rule_editor.field.sop_version")}
            </label>
            <select
              aria-label={copy(
                pageContract,
                "modal.rule_editor.field.sop_version",
              )}
              value={sopVersionId}
              onChange={(e) => setSopVersionId(e.target.value)}
              disabled={sopReadFailed || sopVersions.length === 0}
              title={sopReadFailed ? sopBlockReason : undefined}
            >
              <option value="">
                {sopReadFailed
                  ? copy(pageContract, "modal.rule_editor.option.sops_failed")
                  : sopVersions.length === 0
                    ? copy(pageContract, "modal.rule_editor.option.no_sop")
                    : copy(pageContract, "modal.rule_editor.option.select_sop")}
              </option>
              {sopVersions.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.label}
                </option>
              ))}
            </select>
            <div
              className="muted small"
              style={{ marginTop: 4, lineHeight: 1.45 }}
            >
              {copy(pageContract, "modal.rule_editor.hint.sop_version")}
            </div>

            {isFeedDirection ? (
              <>
                <label>
                  {copy(pageContract, "modal.rule_editor.field.feed_stage")}
                </label>
                <div className="rowf">
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.feed_stage",
                    )}
                    value={feed.animalStage}
                    onChange={(e) =>
                      setFeedField({ animalStage: e.target.value })
                    }
                    disabled={stagePickerDisabled}
                    title={
                      stagesError
                        ? stageBlockReason
                        : stagesSeeded
                          ? undefined
                          : noStagesReason
                    }
                  >
                    <option value={ALL_STAGES_VALUE}>
                      {optionLabel(
                        pageContract,
                        "animal_stage_scope",
                        ALL_STAGES_VALUE,
                      )}
                    </option>
                    {animalStages.map((s) => (
                      <option key={s.code} value={s.code}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.feed_stage",
                    )}
                    value={feed.breedClass}
                    onChange={(e) =>
                      setFeedField({ breedClass: e.target.value })
                    }
                  >
                    {feedClassOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.feed_template")}
                </label>
                <div className="cfgchk">
                  {feedSourceTableOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={feed.sourceTables.includes(option.key)}
                        onChange={() =>
                          toggleFeedArray("sourceTables", option.key)
                        }
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  {feedParameterFamilyOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={feed.parameterFamilies.includes(option.key)}
                        onChange={() =>
                          toggleFeedArray("parameterFamilies", option.key)
                        }
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.feed_dimensions",
                  )}
                </label>
                <div className="cfgchk">
                  {feedDimensionOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={feed.dimensionKeys.includes(option.key)}
                        onChange={() =>
                          toggleFeedArray("dimensionKeys", option.key)
                        }
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.ratio_policy")}
                </label>
                <select
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.ratio_policy",
                  )}
                  value={feed.ratioPolicy}
                  onChange={(e) =>
                    setFeedField({ ratioPolicy: e.target.value })
                  }
                >
                  {feedRatioOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.ration")}
                </label>
                <div className="rowf">
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.table.feed_item",
                    )}
                    value={feed.feedItem}
                    onChange={(e) => setFeedField({ feedItem: e.target.value })}
                  >
                    {feedItemOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.table.quantity",
                    )}
                    type="number"
                    min="0"
                    step="0.01"
                    value={feed.quantity}
                    onChange={(e) =>
                      setFeedField({ quantity: Number(e.target.value) })
                    }
                  />
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.table.quantity",
                    )}
                    value={feed.unit}
                    onChange={(e) => setFeedField({ unit: e.target.value })}
                  >
                    {feedUnitOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.session_timing")}
                </label>
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.session_timing",
                  )}
                  value={feed.sessionTimes}
                  onChange={(e) =>
                    setFeedField({ sessionTimes: e.target.value })
                  }
                  placeholder={copy(
                    pageContract,
                    "modal.rule_editor.placeholder.session_timing",
                  )}
                />
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.session_weights",
                  )}
                  value={feed.slotWeights}
                  onChange={(e) =>
                    setFeedField({ slotWeights: e.target.value })
                  }
                  placeholder={copy(
                    pageContract,
                    "modal.rule_editor.placeholder.session_weights",
                  )}
                  style={{ marginTop: 6 }}
                />

                <label>
                  {copy(pageContract, "modal.rule_editor.field.proof")}
                </label>
                <div className="rowf">
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.proof",
                    )}
                    value={feed.packingProofCsv}
                    onChange={(e) =>
                      setFeedField({ packingProofCsv: e.target.value })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.packing_proof",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.proof",
                    )}
                    value={feed.executionProofCsv}
                    onChange={(e) =>
                      setFeedField({ executionProofCsv: e.target.value })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.execution_proof",
                    )}
                  />
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.inventory")}
                </label>
                <select
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.inventory",
                  )}
                  value={feed.inventoryPolicy}
                  onChange={(e) =>
                    setFeedField({ inventoryPolicy: e.target.value })
                  }
                >
                  {feedInventoryOptions.map((p) => (
                    <option key={p.key} value={p.key}>
                      {p.label}
                    </option>
                  ))}
                </select>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.validation")}
                </label>
                <div className="cfgchk">
                  {feedValidationOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={feed.validationChecks.includes(option.key)}
                        onChange={() =>
                          toggleFeedArray("validationChecks", option.key)
                        }
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  {feedCalculationOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={feed.calculationOutputs.includes(option.key)}
                        onChange={() =>
                          toggleFeedArray("calculationOutputs", option.key)
                        }
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.escalation")}
                </label>
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.escalation",
                  )}
                  value={escalation}
                  onChange={(e) => setEscalation(e.target.value)}
                />
              </>
            ) : (
              <>
                <div className="cmh cfgmatrix-head">
                  <CalendarDays className="ic" />
                  <h4 style={{ margin: 0 }}>
                    {copy(pageContract, "modal.rule_editor.matrix_grid_title")}
                  </h4>
                  <span className="tag t-info">
                    {matrixRows.length}{" "}
                    {copy(
                      pageContract,
                      matrixRows.length === 1
                        ? "modal.rule_editor.label.rule_singular"
                        : "modal.rule_editor.label.rule_plural",
                    )}
                  </span>
                  <div className="sp" style={{ flex: 1 }} />
                  <button
                    type="button"
                    className="btn sm"
                    onClick={loadHerdSourceMatrix}
                  >
                    <CalendarDays className="ic" />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.action.load_source_vaccine_matrix",
                    )}
                  </button>
                  <button
                    type="button"
                    className="btn sm"
                    onClick={addMatrixRow}
                    title={copy(
                      pageContract,
                      "modal.rule_editor.title.add_blank_matrix_row",
                    )}
                  >
                    <Plus className="ic" />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.action.add_matrix_row",
                    )}
                  </button>
                  <button
                    type="button"
                    className="btn sm"
                    onClick={copySelectedMatrixRow}
                    title={copy(
                      pageContract,
                      "modal.rule_editor.title.copy_selected_matrix_row",
                    )}
                  >
                    <Copy className="ic" />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.action.copy_selected_matrix_row",
                    )}
                  </button>
                </div>
                <div
                  className="cfgtablewrap cfgmatrix-table"
                  tabIndex={0}
                  role="group"
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.matrix_grid_title",
                  )}
                >
                  <table className="cfgmatrix-grid">
                    <thead>
                      <tr>
                        <th>
                          {copy(pageContract, "modal.rule_editor.table.row")}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.vaccine_code",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.vaccine_name",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.course_type",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.schedule_note",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.dose_amount",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.vial_doses",
                          )}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.revaccination",
                          )}
                        </th>
                        <th>
                          {copy(pageContract, "modal.rule_editor.table.stage")}
                        </th>
                        <th>
                          {copy(
                            pageContract,
                            "modal.rule_editor.table.species",
                          )}
                        </th>
                        <th>
                          {copy(pageContract, "modal.rule_editor.table.sex")}
                        </th>
                        <th>
                          {copy(pageContract, "modal.rule_editor.table.breed")}
                        </th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {matrixRows.map((row, i) => {
                        const selected = row.id === selectedMatrixRow.id;
                        return (
                          <tr
                            key={row.id}
                            tabIndex={0}
                            aria-selected={selected}
                            onClick={() => setSelectedMatrixRowId(row.id)}
                            onKeyDown={(event) => {
                              if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                setSelectedMatrixRowId(row.id);
                              }
                            }}
                            style={{
                              cursor: "pointer",
                              ...(selected
                                ? {
                                    outline: "1px solid var(--brand)",
                                    background: "rgba(120, 210, 72, 0.08)",
                                  }
                                : {}),
                            }}
                          >
                            <td className="mono">{i + 1}</td>
                            <td style={{ minWidth: 130 }}>
                              <input
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.vaccine_code")} ${i + 1}`}
                                value={row.vaccine.code}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    vaccine: {
                                      ...row.vaccine,
                                      code: e.target.value,
                                    },
                                  })
                                }
                              />
                            </td>
                            <td style={{ minWidth: 170 }}>
                              <input
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.vaccine_name")} ${i + 1}`}
                                value={row.vaccine.name}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    vaccine: {
                                      ...row.vaccine,
                                      name: e.target.value,
                                    },
                                  })
                                }
                              />
                            </td>
                            <td style={{ minWidth: 120 }}>
                              <select
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.course_type")} ${i + 1}`}
                                value={row.vaccine.courseType}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    vaccine: {
                                      ...row.vaccine,
                                      courseType: e.target.value,
                                    },
                                  })
                                }
                              >
                                {courseTypeOptions.map((option) => (
                                  <option key={option.key} value={option.key}>
                                    {option.label}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td style={{ minWidth: 220 }}>
                              <div
                                className="muted small"
                                title={copy(
                                  pageContract,
                                  "modal.rule_editor.table.schedule_note_derived_title",
                                )}
                              >
                                <span className="tag t-mut">
                                  {copy(
                                    pageContract,
                                    "modal.rule_editor.table.schedule_note_derived_badge",
                                  )}
                                </span>
                                <div
                                  style={{
                                    marginTop: 4,
                                    whiteSpace: "normal",
                                    lineHeight: 1.35,
                                  }}
                                >
                                  {scheduleNoteDisplay(row)}
                                </div>
                              </div>
                            </td>
                            <td className="mono" style={{ minWidth: 90 }}>
                              {row.doses?.[0]?.doseAmount ?? "-"}
                            </td>
                            <td className="mono" style={{ minWidth: 80 }}>
                              {row.doses?.[0]?.vialDoses || "-"}
                            </td>
                            <td className="mono" style={{ minWidth: 110 }}>
                              {row.doses?.[0]?.revaccinationIntervalDays || "-"}
                            </td>
                            <td style={{ minWidth: 140 }}>
                              <select
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.stage")} ${i + 1}`}
                                value={row.stage}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    stage: e.target.value,
                                  })
                                }
                                disabled={stagePickerDisabled}
                                title={
                                  stagesError
                                    ? stageBlockReason
                                    : stagesSeeded
                                      ? undefined
                                      : noStagesReason
                                }
                              >
                                <option value={ALL_STAGES_VALUE}>
                                  {optionLabel(
                                    pageContract,
                                    "animal_stage_scope",
                                    ALL_STAGES_VALUE,
                                  )}
                                </option>
                                {animalStages.map((s) => (
                                  <option key={s.code} value={s.code}>
                                    {s.label}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td style={{ minWidth: 110 }}>
                              <select
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.species")} ${i + 1}`}
                                value={row.species}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    species: e.target.value,
                                  })
                                }
                              >
                                {speciesOptions.map((s) => (
                                  <option key={s.key} value={s.key}>
                                    {s.label}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td style={{ minWidth: 100 }}>
                              <select
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.sex")} ${i + 1}`}
                                value={row.sex}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    sex: e.target.value,
                                  })
                                }
                              >
                                {sexOptions.map((s) => (
                                  <option key={s.key} value={s.key}>
                                    {s.label}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td style={{ minWidth: 120 }}>
                              <select
                                aria-label={`${copy(pageContract, "modal.rule_editor.table.breed")} ${i + 1}`}
                                value={row.breed}
                                onFocus={() => setSelectedMatrixRowId(row.id)}
                                onChange={(e) =>
                                  patchMatrixRow(row.id, {
                                    breed: e.target.value,
                                  })
                                }
                              >
                                {breedOptions.map((s) => (
                                  <option key={s.key} value={s.key}>
                                    {s.label}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td>
                              <button
                                type="button"
                                className="btn sm"
                                onClick={(event) => {
                                  event.stopPropagation();
                                  removeMatrixRow(row.id);
                                }}
                                disabled={matrixRows.length === 1}
                                aria-label={`${copy(pageContract, "modal.rule_editor.action.remove_matrix_row")} ${i + 1}`}
                                title={
                                  matrixRows.length === 1
                                    ? copy(
                                        pageContract,
                                        "modal.rule_editor.title.keep_one_matrix_row",
                                      )
                                    : copy(
                                        pageContract,
                                        "modal.rule_editor.action.remove_matrix_row",
                                      )
                                }
                                style={
                                  matrixRows.length === 1
                                    ? { opacity: 0.5, cursor: "not-allowed" }
                                    : undefined
                                }
                              >
                                <X className="ic" />
                              </button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                {!stagesSeeded && !stagesError ? (
                  <div
                    className="muted small"
                    style={{ marginTop: 4, lineHeight: 1.45 }}
                  >
                    {copy(pageContract, "modal.rule_editor.hint.no_stages")}
                  </div>
                ) : null}

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.selected_matrix_row_details",
                  )}{" "}
                  · {copy(pageContract, "modal.rule_editor.table.row")}{" "}
                  {selectedMatrixRowIndex + 1}
                </label>
                <div className="rowf">
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.selected_matrix_row_details",
                    )}
                    value={vaccineType}
                    onChange={(e) => setVaccineType(e.target.value)}
                  >
                    {vaccineTypeOptions.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.vaccine_pathogen_class",
                    )}
                    value={vaccinePathogenClass}
                    onChange={(e) => setVaccinePathogenClass(e.target.value)}
                  >
                    {pathogenClassOptions.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.vaccine_course_type",
                    )}
                    value={vaccineCourseType}
                    onChange={(e) => setVaccineCourseType(e.target.value)}
                  >
                    {courseTypeOptions.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    className="btn sm"
                    onClick={applySourcePresetToSelectedRow}
                  >
                    {copy(
                      pageContract,
                      "modal.rule_editor.action.apply_matrix_schedule",
                    )}
                  </button>
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.selected_matrix_row_details",
                    )}
                    value={vaccineInventoryItemId}
                    onChange={(e) => setVaccineInventoryItemId(e.target.value)}
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.vaccine_inventory_item",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.selected_matrix_row_details",
                    )}
                    value={vaccineManufacturer}
                    onChange={(e) => setVaccineManufacturer(e.target.value)}
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.vaccine_manufacturer",
                    )}
                  />
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.selected_matrix_row_details",
                    )}
                    value={vaccineDisease}
                    onChange={(e) => setVaccineDisease(e.target.value)}
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.vaccine_disease",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.selected_matrix_row_details",
                    )}
                    value={vaccineCompatibilityGroup}
                    onChange={(e) =>
                      setVaccineCompatibilityGroup(e.target.value)
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.placeholder.vaccine_compatibility_group",
                    )}
                  />
                </div>

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.shared_eligibility_policy",
                  )}
                </label>
                <select
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.shared_eligibility_policy",
                  )}
                  value={health}
                  onChange={(e) => setHealth(e.target.value)}
                >
                  {healthOptions.map((s) => (
                    <option key={s.key} value={s.key}>
                      {s.label}
                    </option>
                  ))}
                </select>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.lifecycle")}
                </label>
                <div className="rowf">
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.lifecycle",
                    )}
                    value={lifecycle}
                    onChange={(e) => setLifecycle(e.target.value)}
                  >
                    {lifecycleOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.lifecycle",
                    )}
                    value={reproductive}
                    onChange={(e) => setReproductive(e.target.value)}
                  >
                    {reproductiveOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  {excludedReproductiveOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input
                        type="checkbox"
                        checked={excludeReproductiveStates.includes(option.key)}
                        onChange={() => toggleExcludeReproductive(option.key)}
                      />{" "}
                      {option.label}
                    </label>
                  ))}
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.defer")}
                </label>
                <div className="cfgchk">
                  {deferOptions.map((d) => (
                    <label key={d.key}>
                      <input
                        type="checkbox"
                        checked={deferStates.includes(d.key)}
                        onChange={() => toggleDefer(d.key)}
                      />{" "}
                      {d.label}
                    </label>
                  ))}
                </div>
                <label>
                  {copy(pageContract, "modal.rule_editor.field.missed_dose")}
                </label>
                <select
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.missed_dose",
                  )}
                  value={missedDosePolicy}
                  onChange={(e) => setMissedDosePolicy(e.target.value)}
                >
                  {missedDoseOptions.map((m) => (
                    <option key={m.key} value={m.key}>
                      {m.label}
                    </option>
                  ))}
                </select>

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.compatibility_policy",
                  )}
                </label>
                <div className="rowf">
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.live_to_killed_gap",
                    )}
                    type="number"
                    min="0"
                    value={compatibilityPolicy.liveToKilledGapDays}
                    onChange={(e) =>
                      setCompatibilityField({
                        liveToKilledGapDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.live_to_killed_gap",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.killed_to_killed_gap",
                    )}
                    type="number"
                    min="0"
                    value={compatibilityPolicy.killedToKilledGapDays}
                    onChange={(e) =>
                      setCompatibilityField({
                        killedToKilledGapDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.killed_to_killed_gap",
                    )}
                  />
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.live_to_live_gap",
                    )}
                    type="number"
                    min="0"
                    value={compatibilityPolicy.liveToLiveGapDays}
                    onChange={(e) =>
                      setCompatibilityField({
                        liveToLiveGapDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.live_to_live_gap",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.kid_booster_min_gap",
                    )}
                    type="number"
                    min="0"
                    value={compatibilityPolicy.kidBoosterMinGapDays}
                    onChange={(e) =>
                      setCompatibilityField({
                        kidBoosterMinGapDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.kid_booster_min_gap",
                    )}
                  />
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  <label>
                    <input
                      type="checkbox"
                      checked={compatibilityPolicy.bacterialViralSameDayAllowed}
                      onChange={() =>
                        setCompatibilityField({
                          bacterialViralSameDayAllowed:
                            !compatibilityPolicy.bacterialViralSameDayAllowed,
                        })
                      }
                    />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.field.bacterial_viral_same_day",
                    )}
                  </label>
                  <label>
                    <input
                      type="checkbox"
                      checked={
                        compatibilityPolicy.liveKilledViralSameDayAllowed
                      }
                      onChange={() =>
                        setCompatibilityField({
                          liveKilledViralSameDayAllowed:
                            !compatibilityPolicy.liveKilledViralSameDayAllowed,
                        })
                      }
                    />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.field.live_killed_viral_same_day",
                    )}
                  </label>
                </div>

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.procurement_policy",
                  )}
                </label>
                <div className="rowf">
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.warmup_no_vaccination_days",
                    )}
                    type="number"
                    min="0"
                    value={procurementPolicy.warmupNoVaccinationDays}
                    onChange={(e) =>
                      setProcurementField({
                        warmupNoVaccinationDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.warmup_no_vaccination_days",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.kids_normal_schedule_until_weeks",
                    )}
                    type="number"
                    min="0"
                    value={procurementPolicy.kidsNormalScheduleUntilWeeks}
                    onChange={(e) =>
                      setProcurementField({
                        kidsNormalScheduleUntilWeeks: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.kids_normal_schedule_until_weeks",
                    )}
                  />
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.first_wave",
                    )}
                    value={procurementPolicy.firstWave}
                    onChange={(e) =>
                      setProcurementField({ firstWave: e.target.value })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.first_wave",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.second_wave_after_days",
                    )}
                    type="number"
                    min="0"
                    value={procurementPolicy.secondWaveAfterDays}
                    onChange={(e) =>
                      setProcurementField({
                        secondWaveAfterDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.second_wave_after_days",
                    )}
                  />
                </div>
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.goat_second_wave",
                  )}
                  value={procurementPolicy.goatSecondWave}
                  onChange={(e) =>
                    setProcurementField({ goatSecondWave: e.target.value })
                  }
                  placeholder={copy(
                    pageContract,
                    "modal.rule_editor.field.goat_second_wave",
                  )}
                  style={{
                    marginTop: 6,
                    width: "100%",
                    border: "1px solid var(--line)",
                    background: "var(--bg)",
                    color: "var(--ink)",
                    borderRadius: 8,
                    padding: "8px 10px",
                    font: "inherit",
                    fontSize: 13,
                  }}
                />
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.sheep_second_wave",
                  )}
                  value={procurementPolicy.sheepSecondWave}
                  onChange={(e) =>
                    setProcurementField({ sheepSecondWave: e.target.value })
                  }
                  placeholder={copy(
                    pageContract,
                    "modal.rule_editor.field.sheep_second_wave",
                  )}
                  style={{
                    marginTop: 6,
                    width: "100%",
                    border: "1px solid var(--line)",
                    background: "var(--bg)",
                    color: "var(--ink)",
                    borderRadius: 8,
                    padding: "8px 10px",
                    font: "inherit",
                    fontSize: 13,
                  }}
                />
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  <label>
                    <input
                      type="checkbox"
                      checked={procurementPolicy.adultPriorVaccinationAllowed}
                      onChange={() =>
                        setProcurementField({
                          adultPriorVaccinationAllowed:
                            !procurementPolicy.adultPriorVaccinationAllowed,
                        })
                      }
                    />{" "}
                    {copy(
                      pageContract,
                      "modal.rule_editor.field.adult_prior_vaccination_allowed",
                    )}
                  </label>
                </div>

                <label>
                  {copy(
                    pageContract,
                    "modal.rule_editor.field.pregnancy_policy",
                  )}
                </label>
                <div className="rowf">
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.allow_until_pregnancy_month",
                    )}
                    type="number"
                    min="0"
                    value={pregnancyPolicy.allowUntilPregnancyMonth}
                    onChange={(e) =>
                      setPregnancyField({
                        allowUntilPregnancyMonth: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.allow_until_pregnancy_month",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.skip_from_pregnancy_month",
                    )}
                    type="number"
                    min="0"
                    value={pregnancyPolicy.skipFromPregnancyMonth}
                    onChange={(e) =>
                      setPregnancyField({
                        skipFromPregnancyMonth: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.skip_from_pregnancy_month",
                    )}
                  />
                </div>
                <div className="rowf" style={{ marginTop: 6 }}>
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.skip_through_pregnancy_month",
                    )}
                    type="number"
                    min="0"
                    value={pregnancyPolicy.skipThroughPregnancyMonth}
                    onChange={(e) =>
                      setPregnancyField({
                        skipThroughPregnancyMonth: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.skip_through_pregnancy_month",
                    )}
                  />
                  <input
                    aria-label={copy(
                      pageContract,
                      "modal.rule_editor.field.post_delivery_catch_up_days",
                    )}
                    type="number"
                    min="0"
                    value={pregnancyPolicy.postDeliveryCatchUpDays}
                    onChange={(e) =>
                      setPregnancyField({
                        postDeliveryCatchUpDays: Number(e.target.value),
                      })
                    }
                    placeholder={copy(
                      pageContract,
                      "modal.rule_editor.field.post_delivery_catch_up_days",
                    )}
                  />
                </div>

                <label>
                  {copy(pageContract, "modal.rule_editor.field.vaccine_lot")}
                </label>
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.vaccine_lot",
                  )}
                  value={vaccineLotPolicy}
                  onChange={(e) => setVaccineLotPolicy(e.target.value)}
                />

                <label>
                  {copy(pageContract, "modal.rule_editor.field.escalation")}
                </label>
                <input
                  aria-label={copy(
                    pageContract,
                    "modal.rule_editor.field.escalation",
                  )}
                  value={escalation}
                  onChange={(e) => setEscalation(e.target.value)}
                />
              </>
            )}
          </div>

          {/* Live rule_dsl JSONB preview */}
          <div className="cfgdsl-panel">
            <label className="cfgdsl-label">
              {copy(pageContract, "modal.rule_editor.label.rule_dsl")}
            </label>
            <div
              className="cfgjson"
              aria-label={copy(pageContract, "modal.rule_editor.rule_dsl_aria")}
            >
              {JSON.stringify(dsl, null, 2)}
            </div>
            <div
              className="muted small"
              style={{ marginTop: 9, lineHeight: 1.5 }}
            >
              {isFeedDirection
                ? copy(pageContract, "modal.rule_editor.note.feed_dsl")
                : copy(pageContract, "modal.rule_editor.note.vaccination_dsl")}
            </div>
          </div>
        </div>

        {isFeedDirection ? (
          <>
            <div className="cmh cfgsection-head">
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>
                {copy(pageContract, "modal.rule_editor.table.feed_title")}
              </h4>
            </div>
            <div className="cfgtablewrap cfgschedule-table">
              <table>
                <thead>
                  <tr>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.session")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.feed_item")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.quantity")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.weight")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.proof")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.inventory")}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {feed.sessionTimes
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                    .map((session, i) => {
                      const weight =
                        feed.slotWeights
                          .split(",")
                          .map((s) => s.trim())
                          .filter(Boolean)[i] ??
                        copy(pageContract, "label.placeholder");
                      return (
                        <tr key={`${session}-${i}`}>
                          <td className="mono">{session}</td>
                          <td>
                            {optionLabel(
                              pageContract,
                              "feed_items",
                              feed.feedItem,
                            )}
                          </td>
                          <td>
                            {feed.quantity}{" "}
                            {optionLabel(pageContract, "feed_units", feed.unit)}
                          </td>
                          <td className="mono">{weight}</td>
                          <td className="muted small">
                            {copy(
                              pageContract,
                              "modal.rule_editor.table.packing_execution_proof",
                            )}
                          </td>
                          <td className="muted small">
                            {optionLabel(
                              pageContract,
                              "feed_inventory_policies",
                              feed.inventoryPolicy,
                            )}
                          </td>
                        </tr>
                      );
                    })}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <>
            <div className="cmh cfgsection-head">
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>
                {copy(pageContract, "modal.rule_editor.table.schedule_title")}
              </h4>
              <div className="sp" style={{ flex: 1 }} />
              <button
                type="button"
                className="btn sm"
                onClick={addSelectedDose}
              >
                <Plus className="ic" />{" "}
                {copy(pageContract, "modal.rule_editor.action.add_dose")}
              </button>
            </div>
            <div className="cfgtablewrap cfgschedule-table">
              <table>
                <thead>
                  <tr>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.dose")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.trigger")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.offset")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.window")}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.dose_amount",
                      )}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.dose_unit")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.vial_doses")}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.revaccination",
                      )}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.schedule_note",
                      )}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.route_site")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.max_delay")}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.course_lapse",
                      )}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.repeat")}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.repeat_until",
                      )}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.min_gap")}
                    </th>
                    <th>
                      {copy(pageContract, "modal.rule_editor.table.catch_up")}
                    </th>
                    <th
                      title={copy(
                        pageContract,
                        "modal.rule_editor.table.sop_label_title",
                      )}
                    >
                      {copy(pageContract, "modal.rule_editor.table.sop_label")}
                    </th>
                    <th>
                      {copy(
                        pageContract,
                        "modal.rule_editor.table.proof_policy",
                      )}
                    </th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {selectedDoses().map((d, i) => (
                    <tr key={i}>
                      <td style={{ minWidth: 120 }}>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.dose",
                          )}
                          value={d.doseCode}
                          onChange={(e) =>
                            setSelectedDose(i, { doseCode: e.target.value })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.trigger",
                          )}
                          value={d.trigger}
                          onChange={(e) =>
                            setSelectedDose(i, { trigger: e.target.value })
                          }
                        >
                          {triggerOptions.map((t) => (
                            <option key={t.key} value={t.key}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.offset",
                          )}
                          type="number"
                          value={d.offsetDays}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              offsetDays: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.window",
                          )}
                          type="number"
                          value={d.dueWindowDays}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              dueWindowDays: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.dose_amount",
                          )}
                          type="number"
                          min="0"
                          step="0.01"
                          value={d.doseAmount}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              doseAmount: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 90 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.dose_unit",
                          )}
                          value={d.doseUnit}
                          onChange={(e) =>
                            setSelectedDose(i, { doseUnit: e.target.value })
                          }
                        >
                          {doseUnitOptions.map((option) => (
                            <option key={option.key} value={option.key}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.vial_doses",
                          )}
                          type="number"
                          min="0"
                          value={d.vialDoses}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              vialDoses: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.revaccination",
                          )}
                          type="number"
                          min="0"
                          value={d.revaccinationIntervalDays}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              revaccinationIntervalDays: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 210 }}>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.schedule_note",
                          )}
                          value={d.scheduleNote}
                          onChange={(e) =>
                            setSelectedDose(i, { scheduleNote: e.target.value })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.route_site",
                          )}
                          value={d.routeSite}
                          onChange={(e) =>
                            setSelectedDose(i, { routeSite: e.target.value })
                          }
                        >
                          {routeSiteOptions.map((option) => (
                            <option key={option.key} value={option.key}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.max_delay",
                          )}
                          type="number"
                          value={d.maxDelayDays}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              maxDelayDays: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 140 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.course_lapse",
                          )}
                          value={d.courseLapsePolicy}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              courseLapsePolicy: e.target.value,
                            })
                          }
                        >
                          {courseLapseOptions.map((option) => (
                            <option key={option.key} value={option.key}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.repeat",
                          )}
                          value={d.repeat}
                          onChange={(e) =>
                            setSelectedDose(i, { repeat: e.target.value })
                          }
                        >
                          {repeatOptions.map((t) => (
                            <option key={t.key} value={t.key}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.repeat_until",
                          )}
                          value={d.repeatUntilAfterAge}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              repeatUntilAfterAge: e.target.value,
                            })
                          }
                        />
                      </td>
                      <td>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.min_gap",
                          )}
                          type="number"
                          value={d.minGapDays}
                          onChange={(e) =>
                            setSelectedDose(i, {
                              minGapDays: Number(e.target.value),
                            })
                          }
                        />
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.catch_up",
                          )}
                          value={d.catchUp}
                          onChange={(e) =>
                            setSelectedDose(i, { catchUp: e.target.value })
                          }
                        >
                          {catchUpOptions.map((t) => (
                            <option key={t.key} value={t.key}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <select
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.sop_label",
                          )}
                          title={
                            scheduleSopLabelsSeeded
                              ? copy(
                                  pageContract,
                                  "modal.rule_editor.table.sop_label_title",
                                )
                              : noSopLabelsReason
                          }
                          value={d.sopVersion}
                          onChange={(e) =>
                            setSelectedDose(i, { sopVersion: e.target.value })
                          }
                          disabled={!scheduleSopLabelsSeeded}
                        >
                          {!scheduleSopLabelsSeeded ? (
                            <option value="">
                              {copy(
                                pageContract,
                                "modal.rule_editor.table.no_sop_label",
                              )}
                            </option>
                          ) : null}
                          {scheduleSopOptions.map((t) => (
                            <option key={t.key} value={t.key}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <input
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.table.proof_policy",
                          )}
                          value={d.proofCsv}
                          onChange={(e) =>
                            setSelectedDose(i, { proofCsv: e.target.value })
                          }
                        />
                      </td>
                      <td>
                        <button
                          type="button"
                          aria-label={copy(
                            pageContract,
                            "modal.rule_editor.action.remove_dose",
                          )}
                          className="btn sm"
                          onClick={() => removeSelectedDose(i)}
                          disabled={selectedDoses().length === 1}
                          style={
                            selectedDoses().length === 1
                              ? { opacity: 0.5, cursor: "not-allowed" }
                              : undefined
                          }
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
        <div className="cmh cfgsection-head cfgimpact-head">
          <h4 style={{ margin: 0 }}>
            {copy(
              pageContract,
              isFeedDirection
                ? "modal.rule_editor.impact_title_feed"
                : "modal.rule_editor.impact_title_vaccination",
            )}
          </h4>
          <div className="sp" style={{ flex: 1 }} />
          {category === "vaccination" ? (
            <button
              type="button"
              className="btn sm"
              onClick={preview}
              disabled={pending}
            >
              {pending
                ? copy(pageContract, "action.computing")
                : copy(pageContract, "action.preview_impact")}
            </button>
          ) : (
            <span className="muted small">
              {copy(pageContract, "modal.rule_editor.preview_feed_pending")}
            </span>
          )}
        </div>
        <div className="cfgimpact-body">
          {impact ? (
            <>
              <div className="grid g4">
                {[
                  [
                    copy(pageContract, "modal.rule_editor.kpi.eligible_goats"),
                    impact.eligible_goats,
                  ],
                  [
                    copy(pageContract, "modal.rule_editor.kpi.obligations"),
                    impact.obligations,
                  ],
                  [
                    copy(pageContract, "modal.rule_editor.kpi.batches"),
                    impact.batches,
                  ],
                  [
                    copy(pageContract, "modal.rule_editor.kpi.doses_required"),
                    impact.doses_required,
                  ],
                ].map(([l, v]) => (
                  <div key={String(l)} className="kpi">
                    <div className="lab">{l}</div>
                    <div className="val">{String(v)}</div>
                  </div>
                ))}
              </div>
              <div className="muted small" style={{ marginTop: 10 }}>
                {copy(pageContract, "modal.rule_editor.label.doses_available")}:{" "}
                <b>
                  {impact.doses_available ||
                    copy(pageContract, "label.placeholder")}
                </b>
                {impact.earliest_expiry
                  ? ` · ${copy(pageContract, "modal.rule_editor.label.earliest_expiry")} ${impact.earliest_expiry.slice(0, 10)}`
                  : ""}
              </div>
              {impact.warnings.length > 0
                ? impact.warnings.map((w, i) => (
                    <div
                      key={i}
                      className="alert warn"
                      style={{ marginTop: 8 }}
                    >
                      <AlertTriangle className="ic" />
                      <div>{w}</div>
                    </div>
                  ))
                : null}
              {category === "vaccination" ? impactExplanation() : null}
            </>
          ) : (
            <>
              <p className="muted small">
                {copy(
                  pageContract,
                  isFeedDirection
                    ? "modal.rule_editor.preview_empty_feed"
                    : "modal.rule_editor.preview_empty_vaccination",
                )}
              </p>
              {category === "vaccination" ? impactExplanation() : null}
            </>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            {copy(pageContract, "action.cancel")}
          </button>
          <div className="sp" style={{ flex: 1 }} />
          {versionId ? (
            <span className="muted small">
              {copy(pageContract, "modal.rule_editor.label.draft_saved")} ·{" "}
              {versionId.slice(0, 8)}
            </span>
          ) : null}
          <button
            type="button"
            className="btn"
            onClick={save}
            disabled={pending || !!stageBlockReason || !!sopBlockReason}
            title={stageBlockReason || sopBlockReason || undefined}
            style={
              stageBlockReason || sopBlockReason ? { opacity: 0.45 } : undefined
            }
          >
            {pending
              ? copy(pageContract, "action.saving")
              : copy(pageContract, "action.save_draft")}
          </button>
          <button
            type="button"
            className="btn p"
            onClick={publish}
            disabled={publishDisabled}
            title={publishBlock}
            style={publishDisabled ? { opacity: 0.45 } : undefined}
          >
            {canPublish
              ? copy(pageContract, "action.publish")
              : copy(pageContract, "action.publish_ceo")}
          </button>
        </div>
      </div>
    </>
  );
}
