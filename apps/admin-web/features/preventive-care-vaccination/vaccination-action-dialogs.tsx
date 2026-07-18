import Link from "@/components/no-prefetch-link";
import { CalendarDays } from "lucide-react";
import { scopeHref, type Scope } from "@/lib/scope";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

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
    <Link
      href={scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year) })}
      className={`btn${active ? " p" : ""}`}
      aria-current={active ? "page" : undefined}
      prefetch={false}
    >
      <CalendarDays className="ic" style={{ width: 15 }} aria-hidden="true" />
      {copy(pageContract, "action.open_full_schedule")}
    </Link>
  );
}
