/**
 * The stage words a reader sees. Mirrors backend counts/domain.StageDisplayLabel — the SAME rule
 * Counts Breakdown's facets already apply on the server: only the fattening family ("F2",
 * "F2-Male", "F2-Female") shows its tenant-configured name from animal_stage_lookup; every other
 * code is the farm's own word and stays as stored. An F2 code with no configured name keeps its
 * code rather than an invented word.
 *
 * Presentation only: keys, filter values, sorting and every number are untouched. Pages whose
 * response does not carry the mapped label (Mortality, Farm born) build the name map from the
 * tenant stage vocabulary (`listAnimalStages`) — the read Herd Register and Counts Breakdown
 * already make — and pass it through `stageDisplayLabel`.
 */
export type StageNameMap = ReadonlyMap<string, string>;

export function isFatteningStage(stageCode: string): boolean {
  switch (stageCode.trim().toLowerCase()) {
    case "f2":
    case "f2-male":
    case "f2-female":
      return true;
    default:
      return false;
  }
}

/** Builds the code -> configured name map from the animal-stages read (empty when unavailable). */
export function stageNameMap(items: readonly { stage_code: string; name?: string | null }[] | undefined): StageNameMap {
  return new Map((items ?? []).map((item) => [item.stage_code.trim().toLowerCase(), item.name ?? ""] as const));
}

export function stageDisplayLabel(label: string, names: StageNameMap): string {
  if (!isFatteningStage(label)) return label;
  const name = (names.get(label.trim().toLowerCase()) ?? "").trim();
  return name === "" ? label : name;
}

/**
 * The tenant's configured name for ANY stage code in its vocabulary (animal_stage_lookup: "K0" ->
 * "Newborn", "K3" -> "Weaned kids", "F2" -> "Fattening" …), the code itself when the vocabulary
 * has no entry, and a label that is not a stage code (Farm value's "Adult females") untouched.
 * Where a raw code reaches a reader — Farm value's by-category buckets are keyed by stage code —
 * the vocabulary supplies the word; nothing here invents one.
 */
export function stageVocabularyLabel(label: string, names: StageNameMap): string {
  const name = (names.get(label.trim().toLowerCase()) ?? "").trim();
  return name === "" ? stageDisplayLabel(label, names) : name;
}
