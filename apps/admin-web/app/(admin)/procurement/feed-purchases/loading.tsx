import { FeedPurchasesSkeleton } from "@/features/procurement/feed-purchases-skeletons";

/** /procurement/feed-purchases: the page's own twin (its strip and rows are the UrlSuspense fallbacks too). */
export default function Loading() {
  return <FeedPurchasesSkeleton />;
}
