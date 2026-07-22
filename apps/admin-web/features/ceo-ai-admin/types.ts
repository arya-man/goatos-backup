// Shape of the admin-only step-trace debug payload returned by the backend
// GET /ceo-ai/admin/trace/{request_id} (proxied via
// /api/ceo-ai/admin/trace/{request_id}). This mirrors the backend
// observability.TraceRecord: it is INTERNAL and only ever rendered on this
// admin surface, never in the leadership chat answer.

export type StepTrace = {
  sub_question: string;
  route: string;
  tool_name: string;
  params?: string;
  row_count: number;
  duration_ms: number;
  verdict?: string;
  err?: string;
};

export type TraceRecord = {
  request_id: string;
  actor_role: string;
  conversation_id?: string;
  question_redacted: string;
  route_tier: string;
  tool_called: string;
  source_views: string[] | null;
  row_count: number;
  latency_ms: number;
  status: string;
  rejection_reason?: string;
  review_verdict?: string;
  model_version?: string;
  prompt_version?: string;
  steps: StepTrace[] | null;
  created_at: string;
};

export type TraceError = {
  error: string;
  message?: string;
};
