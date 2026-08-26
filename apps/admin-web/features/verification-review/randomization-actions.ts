"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { setVerificationSamplingPolicy } from "@/lib/api/server";

const PATHNAME = "/verify";

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("vr_status", status);
  url.searchParams.set("vr_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

/**
 * Set one module's share of proof to review (PUT /verification/sampling/{category}).
 *
 * A BLANK FIELD IS NOT A ZERO, and 0 IS NOT A DEFAULT. Zero is a real setting ("review none of this
 * module today"), so a missing or non-numeric entry is refused here rather than sent as 0 — which
 * would silently switch a module's review off. An out-of-range number is NOT clamped either: it
 * goes to the backend, which owns that refusal, so the CEO is told rather than quietly given 100.
 */
export async function setVerificationSamplingPolicyAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const category = String(formData.get("category") ?? "").trim();
  const raw = String(formData.get("sample_percent") ?? "").trim();
  if (!category) {
    redirect(withFeedback(url, "error", "missing_category"));
  }
  if (raw === "" || !/^\d+$/.test(raw)) {
    redirect(withFeedback(url, "error", "invalid_sample_percent"));
  }

  // Idempotency identity for this change. DERIVED, not random, so a double-click or a retried
  // Server Action is ONE write. The business day is part of it: without it, "back to 40% next
  // week" would carry the same key as last week's 40% and be dropped as a replay. The day comes
  // from the panel the CEO is looking at, so a stale page simply produces a different key and
  // writes normally rather than colliding with today's.
  const businessDate = String(formData.get("business_date") ?? "").trim();
  const idempotencyKey = `verification-sampling-${category}-${businessDate}-${raw}`;
  const result = await setVerificationSamplingPolicy(category, Number(raw), idempotencyKey);
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code || "save_failed"));
  }
  // The share decides which items reach the verifier's queue, so the queue page itself is stale the
  // moment it changes.
  revalidatePath(PATHNAME);
  redirect(withFeedback(url, "success", "sampling_saved"));
}
