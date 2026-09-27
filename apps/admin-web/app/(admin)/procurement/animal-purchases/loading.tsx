import { AnimalPurchasesSkeleton } from "@/features/procurement/animal-purchases-skeletons";

/** /procurement/animal-purchases: the page's own twin (its loads rows and animal cards are the UrlSuspense fallbacks too). */
export default function Loading() {
  return <AnimalPurchasesSkeleton />;
}
