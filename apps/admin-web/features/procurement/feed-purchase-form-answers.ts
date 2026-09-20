// THE FEED PURCHASE FORM IS AUTHORED (maintainer decision 2026-09-20): the drawer's purpose-built
// inputs keep their own names, and this maps each one onto the QUESTION id the published form
// knows it by, so the answers the backend checks include the typed values a compulsory question
// asks for. Anything the farm authored beyond these rides as `sop.<id>` and needs no mapping.
const TYPED_QUESTION_FIELDS: Array<[question: string, field: string]> = [
  ["purchase_date", "purchase_date"],
  ["farm_label", "farm"],
  ["feed_item_label", "feed_item"],
  ["quantity_kg", "quantity_kg"],
  ["batch_no", "batch_no"],
  ["vendor", "vendor"],
  ["feed_cost", "feed_cost"],
  ["transport_cost", "transport_cost"],
  ["loading_cost", "loading_cost"],
  ["unloading_cost", "unloading_cost"],
  ["total_cost", "total_cost"],
  ["payment_released", "payment_released"],
  ["payment_status", "payment_status"],
  ["reached_on", "reached_on"],
  ["reached_weight_kg", "reached_weight_kg"],
];

/**
 * readFormAnswers collects what the AUTHORED form asked: every typed value under its question id
 * plus every `sop.<id>` extra the document added. Returns null when the drawer rendered no form
 * version -- an older page, or the form read failing -- in which case the write goes through on
 * its typed fields alone, exactly as it did before the form existed.
 */
export function readFormAnswers(formData: FormData): { answers: Record<string, string>; questionnaire_version: number } | null {
  const version = Number((formData.get("questionnaire_version")?.toString() ?? "").trim());
  if (!Number.isFinite(version) || version <= 0) return null;
  const asked = new Set(formData.getAll("questionnaire_question").map(String));
  const answers: Record<string, string> = {};
  for (const [question, field] of TYPED_QUESTION_FIELDS) {
    const value = (formData.get(field)?.toString() ?? "").trim();
    if (asked.has(question) && value !== "") answers[question] = value;
  }
  for (const [key, value] of formData.entries()) {
    if (!key.startsWith("sop.")) continue;
    const id = key.slice(4);
    const text = value.toString().trim();
    if (!id || text === "") continue;
    // A pick-many question posts one entry per ticked choice; the wire shape is the `|`-joined
    // list the backend's own validator splits on.
    answers[id] = answers[id] ? `${answers[id]}|${text}` : text;
  }
  return { answers, questionnaire_version: version };
}

