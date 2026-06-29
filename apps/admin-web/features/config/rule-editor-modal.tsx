"use client";

import { useMemo, useState, useTransition } from "react";
import { AlertTriangle, CalendarDays, Pencil, Plus, X } from "lucide-react";
import { publishVersion, runImpactPreview, saveDraft, type ActionResult } from "./config-actions";
import {
  buildRuleDsl,
  hasProofRequirement,
  sourceBadge,
  validatePublish,
  ALL_STAGES_VALUE,
  newFeedFields,
  type AnimalStageOption,
  type DoseRow,
  type FeedFields,
  type RuleInput,
  type SopVersionOption,
} from "./rule-dsl";
import type { ImpactPreviewResult } from "@/lib/api/server";
import { copy, optionalCopy, optionGroup, optionLabel, optionalOptionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";

const TONE_CLASS = { warn: "t-warn", info: "t-info", ok: "t-ok" } as const;

export function RuleEditorModal({
  open,
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
  const protocolPlaceholders = optionGroup(pageContract, "protocol_placeholders");
  const animalStageScope = optionGroup(pageContract, "animal_stage_scope");
  const sexOptions = optionGroup(pageContract, "rule_sexes");
  const breedOptions = optionGroup(pageContract, "rule_breeds");
  const healthOptions = optionGroup(pageContract, "rule_healths");
  const lifecycleOptions = optionGroup(pageContract, "rule_lifecycles");
  const reproductiveOptions = optionGroup(pageContract, "rule_reproductive");
  const deferOptions = optionGroup(pageContract, "defer_states");
  const missedDoseOptions = optionGroup(pageContract, "missed_dose_policies");
  const sourceSystemOptions = optionGroup(pageContract, "source_systems");
  const reviewStatusOptions = optionGroup(pageContract, "review_statuses");
  const triggerOptions = optionGroup(pageContract, "trigger_types");
  const repeatOptions = optionGroup(pageContract, "repeat_policies");
  const catchUpOptions = optionGroup(pageContract, "catch_up_policies");
  const scheduleSopOptions = optionGroup(pageContract, "schedule_sop_labels");
  const feedClassOptions = optionalOptionGroup(pageContract, "feed_classes");
  const feedItemOptions = optionalOptionGroup(pageContract, "feed_items");
  const feedUnitOptions = optionalOptionGroup(pageContract, "feed_units");
  const feedInventoryOptions = optionalOptionGroup(pageContract, "feed_inventory_policies");
  const feedSourceTableOptions = optionalOptionGroup(pageContract, "feed_source_tables");
  const feedParameterFamilyOptions = optionalOptionGroup(pageContract, "feed_parameter_families");
  const feedDimensionOptions = optionalOptionGroup(pageContract, "feed_dimension_keys");
  const feedRatioOptions = optionalOptionGroup(pageContract, "feed_ratio_policies");
  const feedValidationOptions = optionalOptionGroup(pageContract, "feed_validation_checks");
  const feedCalculationOptions = optionalOptionGroup(pageContract, "feed_calculation_outputs");
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
  const visibleRuleCategories = ruleCategories.filter((option) => option.key !== "feed_direction" || supportsFeedDirection);

  function firstKey(options: AdminUiOption[], groupId: string): string {
    const [first] = options;
    if (!first) throw new Error(`Admin-web page contract ${pageContract.route_id} has empty option group ${groupId}`);
    return first.key;
  }
  function firstKeyOrEmpty(options: AdminUiOption[]): string {
    return options[0]?.key ?? "";
  }
  function requireKey(options: AdminUiOption[], key: string, groupId: string): string {
    if (!options.some((option) => option.key === key)) {
      throw new Error(`Admin-web page contract ${pageContract.route_id} missing option ${groupId}.${key}`);
    }
    return key;
  }
  function firstKeyOr(options: AdminUiOption[], fallback: string): string {
    return options[0]?.key ?? fallback;
  }
  function categoryDefault(): string {
    const fallback = visibleRuleCategories.find((option) => option.key === "vaccination")?.key ?? visibleRuleCategories[0]?.key ?? initialCategory;
    if (initialCategory === "feed_direction" && !supportsFeedDirection) return fallback;
    return visibleRuleCategories.some((option) => option.key === initialCategory) ? initialCategory : fallback;
  }
  function defaultEscalation(categoryKey: string): string {
    return categoryKey === "feed_direction" && supportsFeedDirection
      ? (optionalCopy(pageContract, "modal.rule_editor.default_feed_escalation") ?? copy(pageContract, "modal.rule_editor.default_vaccination_escalation"))
      : copy(pageContract, "modal.rule_editor.default_vaccination_escalation");
  }
  function defaultSourceSystem(categoryKey: string): string {
    if (categoryKey === "feed_direction" && sourceSystemOptions.some((option) => option.key === "feed_direction_config_pack")) {
      return "feed_direction_config_pack";
    }
    return firstKey(sourceSystemOptions, "source_systems");
  }
  function protocolPlaceholder(categoryKey: string, field: "code" | "name"): string {
    const key = `${categoryKey}.${field}`;
    const value = protocolPlaceholders.find((option) => option.key === key)?.label;
    if (!value) throw new Error(`Admin-web page contract ${pageContract.route_id} missing option protocol_placeholders.${key}`);
    return value;
  }
  function newContractDose(seq: number): DoseRow {
    const isPrimary = seq === 1;
    return {
      doseCode: isPrimary ? copy(pageContract, "modal.rule_editor.default_dose_primary") : `${copy(pageContract, "modal.rule_editor.default_dose_prefix")}${seq}`,
      trigger: isPrimary ? requireKey(triggerOptions, "birth_age", "trigger_types") : requireKey(triggerOptions, "after_previous_completion", "trigger_types"),
      offsetDays: isPrimary ? 21 : 30,
      dueWindowDays: 7,
      repeat: firstKey(repeatOptions, "repeat_policies"),
      repeatUntilAfterAge: copy(pageContract, "modal.rule_editor.default_repeat_until"),
      minGapDays: isPrimary ? 0 : 14,
      catchUp: requireKey(catchUpOptions, "phc_approval", "catch_up_policies"),
      sopVersion: firstKeyOrEmpty(scheduleSopOptions),
      proofCsv: copy(pageContract, "modal.rule_editor.default_proof_policy"),
    };
  }
  function newContractFeedFields(): FeedFields {
    const fallback = newFeedFields();
    const quantity = Number(optionalCopy(pageContract, "modal.rule_editor.default_feed_quantity") ?? fallback.quantity);
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
      sessionTimes: optionalCopy(pageContract, "modal.rule_editor.placeholder.session_timing") ?? fallback.sessionTimes,
      slotWeights: optionalCopy(pageContract, "modal.rule_editor.placeholder.session_weights") ?? fallback.slotWeights,
      packingProofCsv: optionalCopy(pageContract, "modal.rule_editor.placeholder.packing_proof") ?? fallback.packingProofCsv,
      executionProofCsv: optionalCopy(pageContract, "modal.rule_editor.placeholder.execution_proof") ?? fallback.executionProofCsv,
      inventoryPolicy: firstKeyOr(feedInventoryOptions, fallback.inventoryPolicy),
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
  const noStagesReason = copy(pageContract, "modal.rule_editor.no_stages_reason");
  const noSopLabelsReason = copy(pageContract, "modal.rule_editor.no_sop_labels_reason");
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
  const [sopVersionId, setSopVersionId] = useState("");

  // Default to the first backend stage band (lowest sort_order) when seeded, else the ALL_STAGES UI
  // filter. No hardcoded K1 default — the stage vocabulary is owned by animal_stage_lookup.
  const [stage, setStage] = useState(() => animalStages[0]?.code ?? firstKey(animalStageScope, "animal_stage_scope"));
  const [sex, setSex] = useState(firstKey(sexOptions, "rule_sexes"));
  const [breed, setBreed] = useState(firstKey(breedOptions, "rule_breeds"));
  const [health, setHealth] = useState(requireKey(healthOptions, "any", "rule_healths"));
  const [lifecycle, setLifecycle] = useState(firstKey(lifecycleOptions, "rule_lifecycles"));
  const [reproductive, setReproductive] = useState(firstKey(reproductiveOptions, "rule_reproductive"));
  const [deferStates, setDeferStates] = useState<string[]>(() => deferOptions.map((option) => option.key));
  const [vaccineLotPolicy, setVaccineLotPolicy] = useState(copy(pageContract, "modal.rule_editor.default_vaccine_lot_policy"));

  const [missedDosePolicy, setMissedDosePolicy] = useState(requireKey(missedDoseOptions, "phc_approval", "missed_dose_policies"));
  const [escalation, setEscalation] = useState(() => defaultEscalation(defaultCategory));

  const [sourceSystem, setSourceSystem] = useState(() => defaultSourceSystem(defaultCategory));
  const [sourceRef, setSourceRef] = useState("");
  const [reviewStatus, setReviewStatus] = useState(firstKey(reviewStatusOptions, "review_statuses"));
  const [reviewedBy, setReviewedBy] = useState("");
  const [approvedBy, setApprovedBy] = useState("");
  const [approvedAt, setApprovedAt] = useState("");

  const [doses, setDoses] = useState<DoseRow[]>(() => [newContractDose(1)]);
  const [feed, setFeed] = useState<FeedFields>(() => newContractFeedFields());

  const [versionId, setVersionId] = useState("");
  // savedSig is the input signature persisted by the last successful Save. The saved DRAFT version
  // carries sop_version_id + proof_policy as they were AT SAVE TIME; Publish acts on that stored
  // version, not the live form. So editing SOP/proof/source/schedule after saving makes the form
  // "dirty" — Publish must be re-gated until a fresh Save persists the new values, else the backend
  // rejects (e.g. missing sop_version_id) on a version that no longer matches the form.
  const [savedSig, setSavedSig] = useState("");
  const [notice, setNotice] = useState<ActionResult | null>(null);
  const [impact, setImpact] = useState<ImpactPreviewResult | null>(null);
  const [pending, startTransition] = useTransition();
  const isFeedDirection = category === "feed_direction" && supportsFeedDirection;

  const input: RuleInput = useMemo(
    () => ({
      category,
      code,
      name,
      scope,
      effectiveFrom,
      sopVersionId,
      eligibility: { stage, sex, breed, lifecycle, health, reproductive, deferStates },
      vaccineLotPolicy,
      missedDosePolicy,
      escalation,
      source: { sourceSystem, sourceRef, reviewStatus, reviewedBy, approvedBy, approvedAt },
      doses,
      feed,
    }),
    [
      category, code, name, scope, effectiveFrom, sopVersionId, stage, sex, breed, lifecycle, health, reproductive,
      deferStates, vaccineLotPolicy, missedDosePolicy, escalation, sourceSystem, sourceRef,
      reviewStatus, reviewedBy, approvedBy, approvedAt, doses, feed,
    ],
  );

  const dsl = useMemo(() => buildRuleDsl(input), [input]);
  const publishableSourceKeys = new Set(sourceSystemOptions.filter((option) => option.tone === "ok").map((option) => option.key));
  const badge = sourceBadge(input.source, sourceSystemOptions, {
    notSourceBacked: copy(pageContract, "modal.rule_editor.source_badge.not_source_backed"),
    notPublishable: copy(pageContract, "modal.rule_editor.source_badge.not_publishable"),
    approved: copy(pageContract, "modal.rule_editor.source_badge.approved"),
    pending: copy(pageContract, "modal.rule_editor.source_badge.pending"),
    sourceRefNeeded: copy(pageContract, "modal.rule_editor.source_badge.source_ref_needed"),
  });
  const publishGate = validatePublish(input.source, publishableSourceKeys, {
    sourceSystem: copy(pageContract, "modal.rule_editor.publish_block.source_system"),
    sourceRef: copy(pageContract, "modal.rule_editor.publish_block.source_ref"),
    reviewStatus: copy(pageContract, "modal.rule_editor.publish_block.review_status"),
    approvedBy: copy(pageContract, "modal.rule_editor.publish_block.approved_by"),
    approvedAt: copy(pageContract, "modal.rule_editor.publish_block.approved_at"),
    approvedAtRFC3339: copy(pageContract, "modal.rule_editor.publish_block.approved_at_rfc"),
  });
  const inputSig = useMemo(() => JSON.stringify(input), [input]);
  // dirty = saved once, but the form has changed since — the stored version is stale for publish.
  const dirty = versionId !== "" && inputSig !== savedSig;
  const proofOk = hasProofRequirement(input);
  // The Publish gate, in priority order, so the title explains the first blocking reason. A failed
  // stage-reference read blocks first: we cannot trust eligibility authoring if the stage set is unknown.
  const publishBlock = categoryBlockReason
    ? categoryBlockReason
    : stageBlockReason
    ? stageBlockReason
    : sopBlockReason
    ? sopBlockReason
    : !canPublish
    ? publishDisabledReason || copy(pageContract, "modal.rule_editor.only_ceo_publish")
    : !versionId
      ? copy(pageContract, "modal.rule_editor.save_first")
      : dirty
        ? copy(pageContract, "modal.rule_editor.dirty_publish")
        : !sopVersionId
          ? copy(pageContract, "modal.rule_editor.select_sop_publish")
          : !proofOk
            ? copy(pageContract, "modal.rule_editor.proof_publish")
            : !publishGate.ok
              ? publishGate.message
              : "";
  const publishDisabled = pending || publishBlock !== "";

  function setDose(i: number, patch: Partial<DoseRow>) {
    setDoses((rows) => rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  }
  function toggleDefer(value: string) {
    setDeferStates((prev) => (prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value]));
  }
  function changeCategory(nextCategory: string) {
    setCategory(nextCategory);
    setEscalation(defaultEscalation(nextCategory));
    setSourceSystem(defaultSourceSystem(nextCategory));
  }
  function setFeedField(patch: Partial<FeedFields>) {
    setFeed((prev) => ({ ...prev, ...patch }));
  }
  function toggleFeedArray(field: "sourceTables" | "parameterFamilies" | "dimensionKeys" | "validationChecks" | "calculationOutputs", value: string) {
    setFeed((prev) => {
      const current = prev[field];
      return {
        ...prev,
        [field]: current.includes(value) ? current.filter((item) => item !== value) : [...current, value],
      };
    });
  }

  function preview() {
    if (category !== "vaccination") return;
    startTransition(async () => {
      const res = await runImpactPreview({
        stage,
        sex,
        breed,
        doses_per_goat: doses.length,
        dose_rows: doses.length,
        horizon_days: 30,
      });
      if (res.ok && res.data) setImpact(res.data);
      else setNotice({ ok: false, message: res.message ?? copy(pageContract, "modal.rule_editor.message.preview_failed") });
    });
  }

  function save() {
    const readBlock = categoryBlockReason || stageBlockReason || sopBlockReason;
    if (readBlock) {
      setNotice({ ok: false, message: readBlock });
      return;
    }
    startTransition(async () => {
      const res = await saveDraft(input);
      setNotice(res);
      if (res.ok && res.versionId) {
        setVersionId(res.versionId);
        setSavedSig(inputSig); // snapshot what was persisted, so Publish knows the form is clean
      }
    });
  }

  function publish() {
    startTransition(async () => setNotice(await publishVersion(versionId)));
  }

  if (!open) return null;

  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div className="cfgmodal on" style={{ width: "min(1180px,96vw)" }} role="dialog" aria-modal="true" aria-label={copy(pageContract, "modal.rule_editor.aria")}>
        <div className="cmh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <Pencil className="ic" />
          </span>
          <div>
            <div className="b700">{copy(pageContract, "modal.rule_editor.title")}</div>
            <div className="muted small">
              {copy(pageContract, "modal.rule_editor.subtitle")}
            </div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label={copy(pageContract, "modal.rule_editor.close")}>
            <X className="ic" />
          </button>
        </div>

        <div className="cmb">
          <div className="cfgform">
            {stagesError ? (
              <div className="alert warn" role="alert">
                <AlertTriangle className="ic" />
                <div>
                  {copy(pageContract, "modal.rule_editor.stages_error_prefix")} ({stagesError}). {copy(pageContract, "modal.rule_editor.stages_error_body")}
                </div>
              </div>
            ) : null}
            {sopsError ? (
              <div className="alert warn" role="alert">
                <AlertTriangle className="ic" />
                <div>
                  {copy(pageContract, "modal.rule_editor.sops_error_prefix")} ({sopsError}). {copy(pageContract, "modal.rule_editor.sops_error_body")}
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
                  <span className="tag t-ok">{copy(pageContract, "modal.rule_editor.notice_ok")}</span> {notice.message}
                </div>
              ) : (
                <div className="alert warn">
                  <AlertTriangle className="ic" />
                  <div>{notice.message}</div>
                </div>
              )
            ) : null}

            <label>{copy(pageContract, "modal.rule_editor.field.category")}</label>
            <select
              aria-label={copy(pageContract, "modal.rule_editor.field.category")}
              value={category}
              onChange={(e) => changeCategory(e.target.value)}
              disabled={!categoriesSeeded}
              title={categoryBlockReason || undefined}
            >
              {!categoriesSeeded ? (
                <option value={category}>{copy(pageContract, "modal.rule_editor.option.no_categories")}</option>
              ) : (
                visibleRuleCategories.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.label}
                  </option>
                ))
              )}
            </select>

            <label>{copy(pageContract, "modal.rule_editor.field.protocol")}</label>
            <div className="rowf">
              <input aria-label={copy(pageContract, "modal.rule_editor.field.protocol")} value={code} onChange={(e) => setCode(e.target.value)} placeholder={protocolPlaceholder(category, "code")} />
              <input aria-label={copy(pageContract, "modal.rule_editor.field.protocol")} value={name} onChange={(e) => setName(e.target.value)} placeholder={protocolPlaceholder(category, "name")} />
            </div>

            <label>{copy(pageContract, "modal.rule_editor.field.scope")}</label>
            <div className="rowf">
              <select aria-label={copy(pageContract, "modal.rule_editor.field.scope")} value={scope} onChange={(e) => setScope(e.target.value)}>
                {scopeOptions.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </select>
              <input type="date" aria-label={copy(pageContract, "modal.rule_editor.field.scope")} value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} />
            </div>

            <label>{copy(pageContract, "modal.rule_editor.field.sop_version")}</label>
            <select
              aria-label={copy(pageContract, "modal.rule_editor.field.sop_version")}
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
            <div className="muted small" style={{ marginTop: 4, lineHeight: 1.45 }}>
              {copy(pageContract, "modal.rule_editor.hint.sop_version")}
            </div>

            {isFeedDirection ? (
              <>
                <label>{copy(pageContract, "modal.rule_editor.field.feed_stage")}</label>
                <div className="rowf">
                  <select
                    aria-label={copy(pageContract, "modal.rule_editor.field.feed_stage")}
                    value={feed.animalStage}
                    onChange={(e) => setFeedField({ animalStage: e.target.value })}
                    disabled={stagePickerDisabled}
                    title={stagesError ? stageBlockReason : stagesSeeded ? undefined : noStagesReason}
                  >
                    <option value={ALL_STAGES_VALUE}>{optionLabel(pageContract, "animal_stage_scope", ALL_STAGES_VALUE)}</option>
                    {animalStages.map((s) => (
                      <option key={s.code} value={s.code}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.feed_stage")} value={feed.breedClass} onChange={(e) => setFeedField({ breedClass: e.target.value })}>
                    {feedClassOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.feed_template")}</label>
                <div className="cfgchk">
                  {feedSourceTableOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input type="checkbox" checked={feed.sourceTables.includes(option.key)} onChange={() => toggleFeedArray("sourceTables", option.key)} /> {option.label}
                    </label>
                  ))}
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  {feedParameterFamilyOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input type="checkbox" checked={feed.parameterFamilies.includes(option.key)} onChange={() => toggleFeedArray("parameterFamilies", option.key)} /> {option.label}
                    </label>
                  ))}
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.feed_dimensions")}</label>
                <div className="cfgchk">
                  {feedDimensionOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input type="checkbox" checked={feed.dimensionKeys.includes(option.key)} onChange={() => toggleFeedArray("dimensionKeys", option.key)} /> {option.label}
                    </label>
                  ))}
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.ratio_policy")}</label>
                <select aria-label={copy(pageContract, "modal.rule_editor.field.ratio_policy")} value={feed.ratioPolicy} onChange={(e) => setFeedField({ ratioPolicy: e.target.value })}>
                  {feedRatioOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>

                <label>{copy(pageContract, "modal.rule_editor.field.ration")}</label>
                <div className="rowf">
                  <select aria-label={copy(pageContract, "modal.rule_editor.table.feed_item")} value={feed.feedItem} onChange={(e) => setFeedField({ feedItem: e.target.value })}>
                    {feedItemOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                  <input aria-label={copy(pageContract, "modal.rule_editor.table.quantity")} type="number" min="0" step="0.01" value={feed.quantity} onChange={(e) => setFeedField({ quantity: Number(e.target.value) })} />
                  <select aria-label={copy(pageContract, "modal.rule_editor.table.quantity")} value={feed.unit} onChange={(e) => setFeedField({ unit: e.target.value })}>
                    {feedUnitOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.session_timing")}</label>
                <input
                  aria-label={copy(pageContract, "modal.rule_editor.field.session_timing")}
                  value={feed.sessionTimes}
                  onChange={(e) => setFeedField({ sessionTimes: e.target.value })}
                  placeholder={copy(pageContract, "modal.rule_editor.placeholder.session_timing")}
                />
                <input
                  aria-label={copy(pageContract, "modal.rule_editor.field.session_weights")}
                  value={feed.slotWeights}
                  onChange={(e) => setFeedField({ slotWeights: e.target.value })}
                  placeholder={copy(pageContract, "modal.rule_editor.placeholder.session_weights")}
                  style={{ marginTop: 6 }}
                />

                <label>{copy(pageContract, "modal.rule_editor.field.proof")}</label>
                <div className="rowf">
                  <input
                    aria-label={copy(pageContract, "modal.rule_editor.field.proof")}
                    value={feed.packingProofCsv}
                    onChange={(e) => setFeedField({ packingProofCsv: e.target.value })}
                    placeholder={copy(pageContract, "modal.rule_editor.placeholder.packing_proof")}
                  />
                  <input
                    aria-label={copy(pageContract, "modal.rule_editor.field.proof")}
                    value={feed.executionProofCsv}
                    onChange={(e) => setFeedField({ executionProofCsv: e.target.value })}
                    placeholder={copy(pageContract, "modal.rule_editor.placeholder.execution_proof")}
                  />
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.inventory")}</label>
                <select aria-label={copy(pageContract, "modal.rule_editor.field.inventory")} value={feed.inventoryPolicy} onChange={(e) => setFeedField({ inventoryPolicy: e.target.value })}>
                  {feedInventoryOptions.map((p) => (
                    <option key={p.key} value={p.key}>
                      {p.label}
                    </option>
                  ))}
                </select>

                <label>{copy(pageContract, "modal.rule_editor.field.validation")}</label>
                <div className="cfgchk">
                  {feedValidationOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input type="checkbox" checked={feed.validationChecks.includes(option.key)} onChange={() => toggleFeedArray("validationChecks", option.key)} /> {option.label}
                    </label>
                  ))}
                </div>
                <div className="cfgchk" style={{ marginTop: 7 }}>
                  {feedCalculationOptions.map((option) => (
                    <label key={option.key} title={option.title}>
                      <input type="checkbox" checked={feed.calculationOutputs.includes(option.key)} onChange={() => toggleFeedArray("calculationOutputs", option.key)} /> {option.label}
                    </label>
                  ))}
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.escalation")}</label>
                <input aria-label={copy(pageContract, "modal.rule_editor.field.escalation")} value={escalation} onChange={(e) => setEscalation(e.target.value)} />
              </>
            ) : (
              <>
                <label>{copy(pageContract, "modal.rule_editor.field.vaccination_eligibility")}</label>
                <div className="rowf">
                  <select
                    aria-label={copy(pageContract, "modal.rule_editor.field.vaccination_eligibility")}
                    value={stage}
                    onChange={(e) => setStage(e.target.value)}
                    disabled={stagePickerDisabled}
                    title={stagesError ? stageBlockReason : stagesSeeded ? undefined : noStagesReason}
                  >
                    <option value={ALL_STAGES_VALUE}>{optionLabel(pageContract, "animal_stage_scope", ALL_STAGES_VALUE)}</option>
                    {animalStages.map((s) => (
                      <option key={s.code} value={s.code}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.vaccination_eligibility")} value={sex} onChange={(e) => setSex(e.target.value)}>
                    {sexOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                </div>
                {!stagesSeeded && !stagesError ? (
                  <div className="muted small" style={{ marginTop: 4, lineHeight: 1.45 }}>
                    {copy(pageContract, "modal.rule_editor.hint.no_stages")}
                  </div>
                ) : null}
                <div className="rowf" style={{ marginTop: 6 }}>
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.vaccination_eligibility")} value={breed} onChange={(e) => setBreed(e.target.value)}>
                    {breedOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.vaccination_eligibility")} value={health} onChange={(e) => setHealth(e.target.value)}>
                    {healthOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.lifecycle")}</label>
                <div className="rowf">
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.lifecycle")} value={lifecycle} onChange={(e) => setLifecycle(e.target.value)}>
                    {lifecycleOptions.map((s) => (
                      <option key={s.key} value={s.key}>{s.label}</option>
                    ))}
                  </select>
                  <select aria-label={copy(pageContract, "modal.rule_editor.field.lifecycle")} value={reproductive} onChange={(e) => setReproductive(e.target.value)}>
                    {reproductiveOptions.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </div>

                <label>{copy(pageContract, "modal.rule_editor.field.defer")}</label>
                <div className="cfgchk">
                  {deferOptions.map((d) => (
                    <label key={d.key}>
                      <input type="checkbox" checked={deferStates.includes(d.key)} onChange={() => toggleDefer(d.key)} /> {d.label}
                    </label>
                  ))}
                </div>
                <label>{copy(pageContract, "modal.rule_editor.field.missed_dose")}</label>
                <select aria-label={copy(pageContract, "modal.rule_editor.field.missed_dose")} value={missedDosePolicy} onChange={(e) => setMissedDosePolicy(e.target.value)}>
                  {missedDoseOptions.map((m) => (
                    <option key={m.key} value={m.key}>
                      {m.label}
                    </option>
                  ))}
                </select>

                <label>{copy(pageContract, "modal.rule_editor.field.vaccine_lot")}</label>
                <input aria-label={copy(pageContract, "modal.rule_editor.field.vaccine_lot")} value={vaccineLotPolicy} onChange={(e) => setVaccineLotPolicy(e.target.value)} />

                <label>{copy(pageContract, "modal.rule_editor.field.escalation")}</label>
                <input aria-label={copy(pageContract, "modal.rule_editor.field.escalation")} value={escalation} onChange={(e) => setEscalation(e.target.value)} />
              </>
            )}

            <label>{copy(pageContract, "modal.rule_editor.field.source_review")}</label>
            <div className="rowf">
              <select aria-label={copy(pageContract, "modal.rule_editor.field.source_review")} value={sourceSystem} onChange={(e) => setSourceSystem(e.target.value)}>
                {sourceSystemOptions.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </select>
              <input aria-label={copy(pageContract, "modal.rule_editor.field.source_review")} value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} placeholder={copy(pageContract, "modal.rule_editor.placeholder.source_ref")} />
            </div>
            <div className="rowf" style={{ marginTop: 6 }}>
              <select aria-label={copy(pageContract, "modal.rule_editor.field.source_review")} value={reviewStatus} onChange={(e) => setReviewStatus(e.target.value)}>
                {reviewStatusOptions.map((s) => (
                  <option key={s.key} value={s.key}>{s.label}</option>
                ))}
              </select>
              <input aria-label={copy(pageContract, "modal.rule_editor.field.source_review")} value={reviewedBy} onChange={(e) => setReviewedBy(e.target.value)} placeholder={copy(pageContract, "modal.rule_editor.placeholder.reviewed_by")} />
            </div>
            <input
              aria-label={copy(pageContract, "modal.rule_editor.field.source_review")}
              value={approvedBy}
              onChange={(e) => setApprovedBy(e.target.value)}
              placeholder={copy(pageContract, "modal.rule_editor.placeholder.approved_by")}
              style={{ marginTop: 6, width: "100%", border: "1px solid var(--line)", background: "var(--bg)", color: "var(--ink)", borderRadius: 8, padding: "8px 10px", font: "inherit", fontSize: 13 }}
            />
            <input
              aria-label={copy(pageContract, "modal.rule_editor.field.source_review")}
              value={approvedAt}
              onChange={(e) => setApprovedAt(e.target.value)}
              placeholder={copy(pageContract, "modal.rule_editor.placeholder.approved_at")}
              style={{ marginTop: 6, width: "100%", border: "1px solid var(--line)", background: "var(--bg)", color: "var(--ink)", borderRadius: 8, padding: "8px 10px", font: "inherit", fontSize: 13 }}
            />
          </div>

          {/* Live rule_dsl JSONB preview */}
          <div>
            <label style={{ display: "block", fontSize: 11, fontWeight: 700, color: "var(--muted)", textTransform: "uppercase", letterSpacing: ".4px", marginBottom: 5 }}>
              {copy(pageContract, "modal.rule_editor.label.rule_dsl")}
            </label>
            <div className="cfgjson" aria-label={copy(pageContract, "modal.rule_editor.rule_dsl_aria")}>
              {JSON.stringify(dsl, null, 2)}
            </div>
            <div className="muted small" style={{ marginTop: 9, lineHeight: 1.5 }}>
              {isFeedDirection ? copy(pageContract, "modal.rule_editor.note.feed_dsl") : copy(pageContract, "modal.rule_editor.note.vaccination_dsl")}
            </div>
          </div>
        </div>

        {isFeedDirection ? (
          <>
            <div className="cmh" style={{ borderTop: "1px solid var(--line2)", borderBottom: "1px solid var(--line2)", position: "static" }}>
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>{copy(pageContract, "modal.rule_editor.table.feed_title")}</h4>
              <span className={`tag ${TONE_CLASS[badge.tone]}`}>{badge.text}</span>
            </div>
            <div style={{ overflowX: "auto", padding: "0 18px 6px" }}>
              <table>
                <thead>
                  <tr>
                    <th>{copy(pageContract, "modal.rule_editor.table.session")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.feed_item")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.quantity")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.weight")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.proof")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.inventory")}</th>
                  </tr>
                </thead>
                <tbody>
                  {feed.sessionTimes
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                    .map((session, i) => {
                      const weight = feed.slotWeights.split(",").map((s) => s.trim()).filter(Boolean)[i] ?? copy(pageContract, "label.placeholder");
                      return (
                        <tr key={`${session}-${i}`}>
                          <td className="mono">{session}</td>
                          <td>{optionLabel(pageContract, "feed_items", feed.feedItem)}</td>
                          <td>
                            {feed.quantity} {optionLabel(pageContract, "feed_units", feed.unit)}
                          </td>
                          <td className="mono">{weight}</td>
                          <td className="muted small">{copy(pageContract, "modal.rule_editor.table.packing_execution_proof")}</td>
                          <td className="muted small">{optionLabel(pageContract, "feed_inventory_policies", feed.inventoryPolicy)}</td>
                        </tr>
                      );
                    })}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <>
            <div className="cmh" style={{ borderTop: "1px solid var(--line2)", borderBottom: "1px solid var(--line2)", position: "static" }}>
              <CalendarDays className="ic" />
              <h4 style={{ margin: 0 }}>{copy(pageContract, "modal.rule_editor.table.schedule_title")}</h4>
              <span className={`tag ${TONE_CLASS[badge.tone]}`}>{badge.text}</span>
              <div className="sp" style={{ flex: 1 }} />
              <button type="button" className="btn sm" onClick={() => setDoses((r) => [...r, newContractDose(r.length + 1)])}>
                <Plus className="ic" /> {copy(pageContract, "modal.rule_editor.action.add_dose")}
              </button>
            </div>
            <div style={{ overflowX: "auto", padding: "0 18px 6px" }}>
              <table>
                <thead>
                  <tr>
                    <th>{copy(pageContract, "modal.rule_editor.table.dose")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.trigger")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.offset")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.window")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.repeat")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.repeat_until")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.min_gap")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.catch_up")}</th>
                    <th title={copy(pageContract, "modal.rule_editor.table.sop_label_title")}>{copy(pageContract, "modal.rule_editor.table.sop_label")}</th>
                    <th>{copy(pageContract, "modal.rule_editor.table.proof_policy")}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {doses.map((d, i) => (
                    <tr key={i}>
                      <td style={{ minWidth: 120 }}>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.dose")} value={d.doseCode} onChange={(e) => setDose(i, { doseCode: e.target.value })} />
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <select aria-label={copy(pageContract, "modal.rule_editor.table.trigger")} value={d.trigger} onChange={(e) => setDose(i, { trigger: e.target.value })}>
                          {triggerOptions.map((t) => (
                            <option key={t.key} value={t.key}>{t.label}</option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.offset")} type="number" value={d.offsetDays} onChange={(e) => setDose(i, { offsetDays: Number(e.target.value) })} />
                      </td>
                      <td>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.window")} type="number" value={d.dueWindowDays} onChange={(e) => setDose(i, { dueWindowDays: Number(e.target.value) })} />
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select aria-label={copy(pageContract, "modal.rule_editor.table.repeat")} value={d.repeat} onChange={(e) => setDose(i, { repeat: e.target.value })}>
                          {repeatOptions.map((t) => (
                            <option key={t.key} value={t.key}>{t.label}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.repeat_until")} value={d.repeatUntilAfterAge} onChange={(e) => setDose(i, { repeatUntilAfterAge: e.target.value })} />
                      </td>
                      <td>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.min_gap")} type="number" value={d.minGapDays} onChange={(e) => setDose(i, { minGapDays: Number(e.target.value) })} />
                      </td>
                      <td style={{ minWidth: 120 }}>
                        <select aria-label={copy(pageContract, "modal.rule_editor.table.catch_up")} value={d.catchUp} onChange={(e) => setDose(i, { catchUp: e.target.value })}>
                          {catchUpOptions.map((t) => (
                            <option key={t.key} value={t.key}>{t.label}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 110 }}>
                        <select
                          aria-label={copy(pageContract, "modal.rule_editor.table.sop_label")}
                          title={scheduleSopLabelsSeeded ? copy(pageContract, "modal.rule_editor.table.sop_label_title") : noSopLabelsReason}
                          value={d.sopVersion}
                          onChange={(e) => setDose(i, { sopVersion: e.target.value })}
                          disabled={!scheduleSopLabelsSeeded}
                        >
                          {!scheduleSopLabelsSeeded ? <option value="">{copy(pageContract, "modal.rule_editor.table.no_sop_label")}</option> : null}
                          {scheduleSopOptions.map((t) => (
                            <option key={t.key} value={t.key}>{t.label}</option>
                          ))}
                        </select>
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <input aria-label={copy(pageContract, "modal.rule_editor.table.proof_policy")} value={d.proofCsv} onChange={(e) => setDose(i, { proofCsv: e.target.value })} />
                      </td>
                      <td>
                        <button
                          type="button"
                          aria-label={copy(pageContract, "modal.rule_editor.action.remove_dose")}
                          className="btn sm"
                          onClick={() => setDoses((r) => r.filter((_, idx) => idx !== i))}
                          disabled={doses.length === 1}
                          style={doses.length === 1 ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
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
        <div className="cmh" style={{ borderTop: "1px solid var(--line2)", position: "static" }}>
          <h4 style={{ margin: 0 }}>
            {copy(pageContract, isFeedDirection ? "modal.rule_editor.impact_title_feed" : "modal.rule_editor.impact_title_vaccination")}
          </h4>
          <div className="sp" style={{ flex: 1 }} />
          {category === "vaccination" ? (
            <button type="button" className="btn sm" onClick={preview} disabled={pending}>
              {pending ? copy(pageContract, "action.computing") : copy(pageContract, "action.preview_impact")}
            </button>
          ) : (
            <span className="muted small">{copy(pageContract, "modal.rule_editor.preview_feed_pending")}</span>
          )}
        </div>
        <div style={{ padding: "0 18px 12px" }}>
          {impact ? (
            <>
              <div className="grid g4">
                {[
                  [copy(pageContract, "modal.rule_editor.kpi.eligible_goats"), impact.eligible_goats],
                  [copy(pageContract, "modal.rule_editor.kpi.obligations"), impact.obligations],
                  [copy(pageContract, "modal.rule_editor.kpi.batches"), impact.batches],
                  [copy(pageContract, "modal.rule_editor.kpi.doses_required"), impact.doses_required],
                ].map(([l, v]) => (
                  <div key={String(l)} className="kpi">
                    <div className="lab">{l}</div>
                    <div className="val">{String(v)}</div>
                  </div>
                ))}
              </div>
              <div className="muted small" style={{ marginTop: 10 }}>
                {copy(pageContract, "modal.rule_editor.label.doses_available")}: <b>{impact.doses_available || copy(pageContract, "label.placeholder")}</b>
                {impact.earliest_expiry ? ` · ${copy(pageContract, "modal.rule_editor.label.earliest_expiry")} ${impact.earliest_expiry.slice(0, 10)}` : ""}
              </div>
              {impact.warnings.length > 0
                ? impact.warnings.map((w, i) => (
                    <div key={i} className="alert warn" style={{ marginTop: 8 }}>
                      <AlertTriangle className="ic" />
                      <div>{w}</div>
                    </div>
                  ))
                : null}
            </>
          ) : (
            <p className="muted small">
              {copy(pageContract, isFeedDirection ? "modal.rule_editor.preview_empty_feed" : "modal.rule_editor.preview_empty_vaccination")}
            </p>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            {copy(pageContract, "action.cancel")}
          </button>
          <div className="sp" style={{ flex: 1 }} />
          {versionId ? <span className="muted small">{copy(pageContract, "modal.rule_editor.label.draft_saved")} · {versionId.slice(0, 8)}</span> : null}
          <button
            type="button"
            className="btn"
            onClick={save}
            disabled={pending || !!stageBlockReason || !!sopBlockReason}
            title={stageBlockReason || sopBlockReason || undefined}
            style={stageBlockReason || sopBlockReason ? { opacity: 0.45 } : undefined}
          >
            {pending ? copy(pageContract, "action.saving") : copy(pageContract, "action.save_draft")}
          </button>
          <button
            type="button"
            className="btn p"
            onClick={publish}
            disabled={publishDisabled}
            title={publishBlock}
            style={publishDisabled ? { opacity: 0.45 } : undefined}
          >
            {canPublish ? copy(pageContract, "action.publish") : copy(pageContract, "action.publish_ceo")}
          </button>
        </div>
      </div>
    </>
  );
}
