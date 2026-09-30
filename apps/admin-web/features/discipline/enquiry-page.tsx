import { notFound, redirect } from "next/navigation";
import { ChevronLeft } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getWorkforceEnquiry } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { HrmsFrame } from "./frame";
import { EnquiryReport } from "./enquiry-report";

/** One enquiry: what happened and when, then the report (fill it, or read what was submitted). */
export async function EnquiryPage({ enquiryId, pageContract }: { enquiryId: string; pageContract: AdminUiPageContract }) {
  const t = (key: string) => copy(pageContract, key);
  const result = await getWorkforceEnquiry(enquiryId);
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);
  if (!result.ok && result.error.status === 404) notFound();
  const back = (
    <Link className="btn" href="/people/enquiries">
      <ChevronLeft className="ic" /> {t("detail.back")}
    </Link>
  );
  if (!result.ok) {
    return (
      <HrmsFrame pageContract={pageContract} aside={back}>
        <div className="alert">{result.error.message}</div>
      </HrmsFrame>
    );
  }
  const canSubmit = controlEnabled(pageContract, "submit_enquiry", false);
  const reason = control(pageContract, "submit_enquiry").disabled_reason ?? t("disabled.enquiry_write");
  return (
    <HrmsFrame pageContract={pageContract} aside={back}>
      <EnquiryReport pageContract={pageContract} initial={result.data} canSubmit={canSubmit} submitReason={reason} />
    </HrmsFrame>
  );
}
