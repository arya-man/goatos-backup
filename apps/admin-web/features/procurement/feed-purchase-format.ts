import { optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { Tone } from "@/components/ui-primitives";

/**
 * Resolves a payment state's chip tone and label from the page contract's own option group.
 *
 * The tone lives on the backend option (`feed_purchase_payment_statuses` publishes `ok` for paid
 * and `warn` for pending), so the screen never compares a payment state against a hardcoded
 * "Paid": adding a third state, or renaming one, is a backend change that the chip follows. An
 * unrecognised value renders muted with its own text rather than being silently coloured as if it
 * were a known state.
 */
export function paymentStatusChip(
  pageContract: AdminUiPageContract,
  status: string,
  fallbackLabel: string,
): { tone: Tone; label: string } {
  const option = optionGroup(pageContract, "feed_purchase_payment_statuses").find((item) => item.key === status);
  if (!option) return { tone: "mut", label: status || fallbackLabel };
  return { tone: (option.tone || "mut") as Tone, label: option.label };
}
