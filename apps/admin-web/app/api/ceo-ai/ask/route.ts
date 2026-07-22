import { type NextRequest, NextResponse } from "next/server";
import {
  getCountsBreakdown,
  getAdminWebBootstrap,
  getFeedDirectionPreview,
  getVaccinationShedSummary,
  type ApiResult,
  type CountsBreakdownResponse,
  type FeedDirectionPreviewPage,
  type VaccinationShedSummaryResponse,
} from "@/lib/api/server";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type ToolMode = "mesha-read-api" | "vertex-ai-pending" | "assistant-help";
type AssistantTool =
  | "vaccination_shed_summary"
  | "counts_summary"
  | "feed_direction_summary"
  | "architecture_help";

type AskPayload = {
  question?: unknown;
};

type AskAnswer = {
  answer: string;
  source: string;
  mode: ToolMode;
};

const IST_DATE_FORMAT = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Kolkata",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

export async function POST(request: NextRequest) {
  const leadership = await requireLeadershipAccess();
  if (!leadership.ok) {
    return NextResponse.json({ error: leadership.error }, { status: leadership.status });
  }

  let body: AskPayload;
  try {
    body = (await request.json()) as AskPayload;
  } catch {
    return NextResponse.json({ error: "invalid_json" }, { status: 400 });
  }
  const question = typeof body.question === "string" ? body.question.trim().slice(0, 1200) : "";
  if (!question) {
    return NextResponse.json({ error: "question_required" }, { status: 400 });
  }

  const answer = await answerFromReadTools(question);
  return NextResponse.json(answer, { headers: { "Cache-Control": "no-store" } });
}

async function requireLeadershipAccess(): Promise<{ ok: true } | { ok: false; status: number; error: string }> {
  const bootstrap = await getAdminWebBootstrap();
  if (!bootstrap.ok) {
    return {
      ok: false,
      status: bootstrap.error.status === 401 ? 401 : 403,
      error: bootstrap.error.status === 401 ? "unauthorized" : "leadership_required",
    };
  }

  const actor = bootstrap.data.top_bar?.role_preview;
  const displayName = typeof actor?.display_name === "string" ? actor.display_name : "";
  const subtitle = typeof actor?.subtitle === "string" ? actor.subtitle : "";
  if (isLeadershipActor(displayName, subtitle)) return { ok: true };

  return { ok: false, status: 403, error: "leadership_required" };
}

function isLeadershipActor(displayName: string, subtitle: string): boolean {
  const text = `${displayName} ${subtitle}`.toLowerCase();
  return /\b(ceo|cxo|coo|founder|superadmin)\b/.test(text);
}

async function answerFromReadTools(question: string): Promise<AskAnswer> {
  const q = question.toLowerCase();
  return answerWithTool(routeByKeywords(q), question);
}

function routeByKeywords(q: string): AssistantTool {
  if (/\b(architecture|vertex|gemini|mcp|toolbox|how.*work|what.*build)\b/.test(q)) {
    return "architecture_help";
  }
  if (/\b(feed|ration|packing|kg)\b/.test(q)) {
    return "feed_direction_summary";
  }
  if (/\b(count|counts|goat|animal|herd|shed)\b/.test(q) && !/\b(vaccin|due|overdue)\b/.test(q)) {
    return "counts_summary";
  }
  return "vaccination_shed_summary";
}

async function answerWithTool(tool: AssistantTool, question: string): Promise<AskAnswer> {
  if (tool === "architecture_help") {
    return {
      answer: [
        "Architecture: floating CEO/CXO chat bubble -> Mesha backend -> Gemini via Vertex AI -> allowed Mesha tools -> read-only Mesha APIs/Postgres read models -> answer.",
        "Vertex AI is the Google Cloud doorway to Gemini. Gemini chooses a tool. Mesha executes the tool. MCP Toolbox is useful when we add direct database tools/views; it is not needed for every existing API.",
        "We do not MCP every API. We expose a small allowlist: counts summary, vaccination shed summary, feed summary, then add more tools only when needed.",
      ].join("\n\n"),
      source: "Mesha assistant architecture",
      mode: "assistant-help",
    };
  }

  if (tool === "feed_direction_summary") {
    const result = await getFeedDirectionPreview({
      park_id: "all",
      target_date: todayIst(),
      limit: 8,
      offset: 0,
    });
    if (result.ok) return feedAnswer(result.data);
    return apiErrorAnswer("Feed", result);
  }

  if (tool === "counts_summary") {
    const result = await getCountsBreakdown({ limit: 100, offset: 0 });
    if (result.ok) return countsAnswer(result.data, question);
    return apiErrorAnswer("Counts", result);
  }

  const result = await getVaccinationShedSummary({ limit: 10, offset: 0 });
  if (result.ok) return vaccinationAnswer(result.data, question.toLowerCase());
  return apiErrorAnswer("Vaccination", result);
}

function vaccinationAnswer(data: VaccinationShedSummaryResponse, question: string): AskAnswer {
  const rows = data.rows ?? [];
  const overdue = rows.filter((row) => row.status === "overdue");
  const needsReview = rows.filter((row) => row.status === "needs_review");
  const totalAnimals = rows.reduce((sum, row) => sum + (row.animals ?? 0), 0);
  const totalDue = rows.reduce((sum, row) => sum + (row.due ?? 0), 0);
  const totalDone = rows.reduce((sum, row) => sum + (row.done ?? 0), 0);
  const focusRows = question.includes("overdue") ? overdue : rows.slice(0, 5);
  const rowLines = focusRows
    .slice(0, 5)
    .map((row) => `${row.parkName ?? "Park"} / ${row.shedName}: ${row.due} due, ${row.done} done, status ${label(row.status)}`);

  return {
    answer: [
      `Vaccination snapshot: ${rows.length} sheds loaded, ${totalAnimals} animals, ${totalDue} due, ${totalDone} done.`,
      overdue.length || needsReview.length ? `Attention: ${overdue.length} overdue shed(s), ${needsReview.length} needs-review shed(s).` : "No overdue or needs-review rows in the loaded page.",
      rowLines.length ? rowLines.join("\n") : "No shed rows returned for this scope.",
    ].join("\n\n"),
    source: `Mesha vaccination shed read model · ${todayIst()}`,
    mode: "mesha-read-api",
  };
}

function countsAnswer(data: CountsBreakdownResponse, question: string): AskAnswer {
  const rows = data.items ?? [];
  const shedQuery = extractShedQuery(question, rows);
  if (shedQuery) {
    const matches = rows.filter((row) => normalizeLabel(row.shed_label) === shedQuery);
    if (matches.length) {
      const total = matches.reduce((sum, row) => sum + (row.count ?? 0), 0);
      return {
        answer: `${matches[0]?.shed_label ?? labelTitle(shedQuery)} has ${total} animals in the selected scope.`,
        source: `Mesha counts read model · ${todayIst()}`,
        mode: "mesha-read-api",
      };
    }
  }

  const total = data.total_count ?? rows.reduce((sum, row) => sum + (row.count ?? 0), 0);
  const rowLines = rows
    .slice(0, 6)
    .map((row) => `${row.park_label ?? "Park"} / ${row.shed_label ?? "Shed"}: ${row.count ?? 0} animals`);
  return {
    answer: [`Current count snapshot: ${total} animals in the selected scope.`, rowLines.join("\n") || "No count rows returned."].join("\n\n"),
    source: `Mesha counts read model · ${todayIst()}`,
    mode: "mesha-read-api",
  };
}

function feedAnswer(data: FeedDirectionPreviewPage): AskAnswer {
  const rows = data.items ?? [];
  const blocked = rows.filter((row) => row.blocked).length;
  const rowLines = rows
    .slice(0, 5)
    .map((row) => `${row.park_label ?? "Park"} / ${row.shed_label ?? "Shed"}: ${row.session_label ?? "session"} · ${row.blocked ? "blocked" : "resolved"} · ${row.session_total_kg} kg`);
  return {
    answer: [`Feed direction snapshot: ${rows.length} rows loaded for ${todayIst()}, ${blocked} blocked.`, rowLines.join("\n") || "No feed rows returned."].join("\n\n"),
    source: `Mesha feed direction read model · ${todayIst()}`,
    mode: "mesha-read-api",
  };
}

function apiErrorAnswer<T>(domain: string, result: ApiResult<T>): AskAnswer {
  if (result.ok) {
    return { answer: "No issue.", source: domain, mode: "mesha-read-api" };
  }
  return {
    answer: `${domain} data is not reachable from this session yet: ${result.error.message}`,
    source: "Mesha read API",
    mode: "vertex-ai-pending",
  };
}

function todayIst(): string {
  return IST_DATE_FORMAT.format(new Date());
}

function label(value: string | undefined): string {
  return (value || "unknown").replaceAll("_", " ");
}

function extractShedQuery(
  question: string,
  rows: Array<{ shed_label?: string }>,
): string | null {
  const normalized = normalizeLabel(question);
  const directMatch = rows
    .map((row) => normalizeLabel(row.shed_label))
    .filter(Boolean)
    .sort((a, b) => b.length - a.length)
    .find((shedLabel) => normalized.includes(shedLabel));
  if (directMatch) return directMatch;

  const match = normalized.match(/\b(?:in|at|for)\s+([a-z]+(?:\s+\d+)?(?:\s*-\s*part\s+\d+)?)\b/);
  return match?.[1]?.trim() || null;
}

function normalizeLabel(value: string | undefined): string {
  return (value || "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function labelTitle(value: string): string {
  return value.replace(/\b\w/g, (letter) => letter.toUpperCase());
}
