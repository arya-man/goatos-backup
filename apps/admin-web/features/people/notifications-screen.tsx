import { redirect } from "next/navigation";

import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getNotificationAudienceMatrix } from "@/lib/api/server";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { NotificationMatrix } from "./notification-matrix";

/**
 * The Notifications tab of /people (maintainer decision 2026-09-08): who hears which alert, per
 * designation. Server component; the whole matrix is one bounded read (a few dozen alerts by a
 * dozen job titles), never a paginated list, and the client matrix owns only the tick state
 * until each row is saved.
 */
export async function NotificationsScreen({ pageContract }: { pageContract: AdminUiPageContract }) {
  const result = await getNotificationAudienceMatrix();
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  if (!result.ok) {
    return (
      <div className="callout dng" role="alert" style={{ marginTop: 12 }}>
        <b>{result.error.code ?? result.error.kind}</b>&nbsp;{copy(pageContract, "notifications.error.load")}
      </div>
    );
  }

  return <NotificationMatrix matrix={result.data} pageContract={pageContract} />;
}
