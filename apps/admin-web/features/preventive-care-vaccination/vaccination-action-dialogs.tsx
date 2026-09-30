import { Iconify } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/app/link-button";
import { scopeHref, type Scope } from "@/lib/scope";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

/** Page header action: the template header Button (a link) that opens the full vaccination schedule. */
export function VaccinationFullScheduleButton({
  scope,
  pageContract,
  active,
  year,
}: {
  scope: Scope;
  pageContract: AdminUiPageContract;
  active: boolean;
  year: number;
}) {
  return (
    <LinkButton
      href={scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year) })}
      variant={active ? "contained" : "outlined"}
      color={active ? "primary" : "inherit"}
      aria-current={active ? "page" : undefined}
      startIcon={<Iconify icon="solar:calendar-date-bold" />}
    >
      {copy(pageContract, "action.open_full_schedule")}
    </LinkButton>
  );
}
