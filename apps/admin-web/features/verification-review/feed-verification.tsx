import { getFeedPackingVerificationLog } from "@/lib/api/server";
import { copy } from "@/lib/admin-ui-contract";
import { FeedVerificationView, type FeedVerificationViewProps } from "./feed-verification-view";

/**
 * FEED VERIFICATION for a request whose URL already says the panel is OPEN (a shared deep link, or a
 * date / park change made inside the open panel -- both re-assert the panel key in the query).
 *
 * The page renders this ONLY in that case. A /verify render with the panel closed -- the queue
 * landing, every Accept redirect that advances to the next video -- must not pay for this read; a
 * panel opened by its button loads the same day itself, from the drawer (loadFeedVerificationLogAction).
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "feed_verification", false)` first;
 * the same capability (permissions.VerificationFeedPackingLog) gates the endpoint read here.
 */
export async function FeedVerification({
  feedDay,
  parkId,
  ...view
}: FeedVerificationViewProps & {
  /** The feed day to show, YYYY-MM-DD; absent means today. */
  feedDay?: string;
  /** The page's own top-bar park scope, forwarded to the read. */
  parkId?: string;
}) {
  const result = await getFeedPackingVerificationLog({ feedDay, parkId });
  if (!result.ok) {
    return <div className="small muted">{copy(view.pageContract, "feed_verification.unavailable")}</div>;
  }
  return <FeedVerificationView {...view} log={result.data} />;
}
