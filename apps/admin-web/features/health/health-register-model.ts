import type {
  HealthRegisterDocument,
  HealthRegisterQuestion,
  HealthRegisterRule,
} from "@/lib/api/server";

// The layman's model of a register, derived from the document the engine actually runs.
//
// THE ONE IDEA THE SCREEN HAS TO CONVEY: an answer can be a SIGN, and an illness is
// recognised by signs. Underneath, a sign is a short token an answer produces and a rule
// names -- but a token is an implementation detail, and a vet reading
// `left_stomach:bloating` learns nothing they did not already know. Everything here
// exists to render that token as "Left stomach · Bloated" and to let it be PICKED rather
// than typed.
//
// Nothing here changes the document's shape. It is a lens over it, so the thing the
// engine evaluates and the thing the screen shows can never drift.

/** Where a sign comes from: one answer to one question. */
export type SignSource = {
  questionId: string;
  questionTitle: string;
  answerValue: string;
  answerLabel: string;
};

export type SignIndex = {
  /** token -> every answer that produces it. Usually one; a token may have several. */
  sources: Map<string, SignSource[]>;
  /** token -> the illnesses that read it. */
  usedBy: Map<string, string[]>;
};

/**
 * DERIVED TOKEN for a new sign, so an author never types one.
 *
 * `<question id>:<answer value>` is the shape the seeded register already uses for most
 * of its signs (`left_stomach:bloating`, `neuro:circling`), so a hand-authored one looks
 * like the rest rather than announcing which screen made it.
 */
export function derivedSign(question: HealthRegisterQuestion, answerValue: string): string {
  const q = (question.id || "").trim();
  const a = (answerValue || "").trim();
  if (!q || !a) return "";
  return `${q}:${a}`;
}

export function buildSignIndex(doc: HealthRegisterDocument): SignIndex {
  const sources = new Map<string, SignSource[]>();
  for (const q of doc.questions ?? []) {
    for (const o of q.options ?? []) {
      for (const token of o.emits ?? []) {
        const list = sources.get(token) ?? [];
        list.push({
          questionId: q.id,
          questionTitle: q.title,
          answerValue: o.value,
          answerLabel: o.label,
        });
        sources.set(token, list);
      }
    }
    for (const b of q.bands ?? []) {
      for (const token of b.emits ?? []) {
        const list = sources.get(token) ?? [];
        list.push({
          questionId: q.id,
          questionTitle: q.title,
          answerValue: "",
          answerLabel: bandLabel(b),
        });
        sources.set(token, list);
      }
    }
  }

  const usedBy = new Map<string, string[]>();
  for (const rule of doc.rules ?? []) {
    for (const token of tokensReadBy(rule)) {
      const list = usedBy.get(token) ?? [];
      if (!list.includes(rule.id)) list.push(rule.id);
      usedBy.set(token, list);
    }
  }
  return { sources, usedBy };
}

/** Every token one illness looks at, across its tiers and gates. */
export function tokensReadBy(rule: HealthRegisterRule): string[] {
  const out = new Set<string>();
  for (const tier of [rule.pathognomonic, rule.probable, rule.possible]) {
    for (const clause of tier ?? []) {
      for (const f of clause.findings ?? []) out.add(f);
    }
  }
  for (const f of rule.gate_required ?? []) out.add(f);
  for (const f of rule.gate_excluded ?? []) out.add(f);
  return [...out];
}

function bandLabel(b: { gt?: number; gte?: number; lt?: number; lte?: number }): string {
  const parts: string[] = [];
  if (b.gt !== undefined) parts.push(`over ${b.gt}`);
  if (b.gte !== undefined) parts.push(`${b.gte} and up`);
  if (b.lt !== undefined) parts.push(`under ${b.lt}`);
  if (b.lte !== undefined) parts.push(`up to ${b.lte}`);
  return parts.join(", ") || "any reading";
}

/**
 * A sign in words: "Left stomach · Bloated".
 *
 * A token no answer produces still gets a readable form rather than being hidden. Some
 * are supplied by the herd register (`sex:F` -> "Female"), some by the engine itself,
 * and some are simply not authored yet -- and an author who cannot see a condition
 * cannot fix it.
 */
export function signLabel(token: string, index: SignIndex): string {
  const sources = index.sources.get(token);
  if (sources && sources.length > 0) {
    const first = sources[0];
    const more = sources.length > 1 ? ` +${sources.length - 1}` : "";
    return `${first.questionTitle} · ${first.answerLabel}${more}`;
  }
  return animalFactLabel(token) ?? token;
}

/** Whether a token is something the screen cannot offer as a pickable answer. */
export function isSuppliedElsewhere(token: string): boolean {
  return Boolean(animalFactLabel(token));
}

/**
 * The facts the HERD REGISTER supplies, never the form. They appear in conditions and
 * must read as words, but they are not answers anyone can tick, so the picker does not
 * offer them and the question editor does not claim them.
 */
function animalFactLabel(token: string): string | null {
  const [family, value] = token.split(":", 2);
  if (!value) return null;
  switch (family) {
    case "species":
      return `Species is ${value}`;
    case "sex":
      return value === "F" ? "Female" : value === "M" ? "Male" : `Sex is ${value}`;
    case "status":
      return `Status is ${value.replace(/_/g, " ")}`;
    case "stage":
      return `Stage is ${value}`;
    default:
      return null;
  }
}

/**
 * An illness in words: PREG_TOX -> "Preg tox".
 *
 * The id stays the id -- it is what the engine emits and what a case is recorded under,
 * and the editor keeps a field for it. This is presentation only, so a reader scanning
 * thirty-four illnesses is reading words rather than shouting.
 */
export function illnessLabel(id: string): string {
  const words = (id || "").trim().toLowerCase().replace(/_/g, " ");
  if (!words) return "";
  return words.charAt(0).toUpperCase() + words.slice(1);
}

export type SignChoice = {
  token: string;
  questionTitle: string;
  answerLabel: string;
};

/**
 * Every sign the form can currently produce, grouped by the question that produces it.
 *
 * TWO THINGS THAT LOOK LIKE DETAILS AND ARE NOT:
 *
 * The group is keyed by the question ID, not its title. Two questions may legitimately
 * share a title, and keying by it silently DROPPED one question's signs from the picker.
 *
 * Several answers may produce the SAME sign -- milk, colostrum and watery all mean the
 * udder has milk -- so the rows are collapsed to one entry per token and labelled with
 * every answer that produces it ("Milk / Colostrum / Watery"). Listing them separately
 * offered the author three identical choices and, because a React list keyed by token
 * then had duplicates, made the picker's own rendering undefined.
 */
export function signChoices(doc: HealthRegisterDocument): Map<string, SignChoice[]> {
  const byQuestion = new Map<string, SignChoice[]>();
  for (const q of doc.questions ?? []) {
    const byToken = new Map<string, string[]>();
    for (const o of q.options ?? []) {
      for (const token of o.emits ?? []) {
        byToken.set(token, [...(byToken.get(token) ?? []), o.label]);
      }
    }
    for (const b of q.bands ?? []) {
      for (const token of b.emits ?? []) {
        byToken.set(token, [...(byToken.get(token) ?? []), bandLabel(b)]);
      }
    }
    const rows: SignChoice[] = [...byToken.entries()].map(([token, labels]) => ({
      token,
      questionTitle: q.title,
      answerLabel: [...new Set(labels)].join(" / "),
    }));
    if (rows.length > 0) byQuestion.set(q.id || q.title, rows);
  }
  return byQuestion;
}

/** Adds a sign to an illness as its own single-sign condition, under the given tier. */
export function withSignAdded(
  rule: HealthRegisterRule,
  tier: "pathognomonic" | "probable" | "possible",
  token: string,
): HealthRegisterRule {
  const clauses = rule[tier] ?? [];
  if (clauses.some((c) => (c.findings ?? []).length === 1 && c.findings[0] === token)) {
    return rule;
  }
  return { ...rule, [tier]: [...clauses, { findings: [token] }] };
}
