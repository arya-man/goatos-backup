import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

interface ShedEventActionsProps {
  pageContract: AdminUiPageContract;
}

/**
 * Shed-event drawers are read-only context in admin-web. Dose/proof execution
 * happens on the operator SOP task, and verification happens on completion rows.
 */
export function ShedEventActions({ pageContract }: ShedEventActionsProps) {
  return <div className="note">{copy(pageContract, "form.actions.unavailable")}</div>;
}
