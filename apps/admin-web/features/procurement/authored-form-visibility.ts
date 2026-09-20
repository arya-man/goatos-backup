type ConditionalQuestion = {
  id: string;
  only_if?: { question_id: string; value: string } | null;
};

/** Evaluate the whole document in order; hidden stale parents cannot activate descendants. */
export function visibleQuestionIds(questions: readonly ConditionalQuestion[], answers: Record<string, string>): Set<string> {
  const visible = new Set<string>();
  for (const question of questions) {
    const condition = question.only_if;
    if (condition && (!visible.has(condition.question_id) || (answers[condition.question_id] ?? "").trim() !== condition.value)) continue;
    visible.add(question.id);
  }
  return visible;
}
