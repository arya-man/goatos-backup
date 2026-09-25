export type CountsSummaryCard = {
  key: string;
  label: string;
  count: number;
  detail: string;
  tone: "brand" | "teal" | "amber" | "muted";
};

export type CountsStageSexPoint = {
  key: string;
  label: string;
  female: number;
  male: number;
  other: number;
  count: number;
};

type SexLabels = {
  female: string;
  male: string;
  other: string;
};

type SummaryLabels = {
  fattening: string;
  bucks: string;
  breeding: string;
  icu: string;
  k0: string;
  k1: string;
  k2: string;
  k3: string;
  k4: string;
  /** Label for animals whose stage no card above names (a stage added on Configuration). */
  other?: string;
};

type SummaryBucketKey = "fattening" | "bucks" | "breeding" | "icu" | "k0" | "k1" | "k2" | "k3" | "k4";

type SummaryBucket = {
  female: number;
  male: number;
  other: number;
  count: number;
};

const BUCKET_STAGE_KEYS: Record<Exclude<SummaryBucketKey, "icu">, readonly string[]> = {
  fattening: ["F2", "F2-Male", "F2-Female", "Fattening", "Fattening male", "Fattening female"],
  bucks: ["Buck", "Bucks"],
  breeding: ["Mother", "Mothers", "Milking", "M0", "Warmup", "Pregnant", "Non-Pregnant", "Non Pregnant", "Breeding female", "Breeding stock"],
  k0: ["K0", "K 0"],
  k1: ["K1", "K 1"],
  k2: ["K2", "K 2"],
  k3: ["K3", "K 3"],
  k4: ["K4", "K 4"],
};

function emptyBucket(): SummaryBucket {
  return { female: 0, male: 0, other: 0, count: 0 };
}

function normalizedStage(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "")
    .trim();
}

function matchesStageKey(point: CountsStageSexPoint, aliases: readonly string[]): boolean {
  const key = point.key || "";
  const value = key || point.label || "";
  const normalized = normalizedStage(value);
  return aliases.some((alias) => normalized === normalizedStage(alias));
}

function classifyStage(point: CountsStageSexPoint): SummaryBucketKey | null {
  const value = point.key || point.label || "";
  const normalized = normalizedStage(value);
  if (normalized.includes("icu")) return "icu";
  for (const bucketKey of Object.keys(BUCKET_STAGE_KEYS) as Exclude<SummaryBucketKey, "icu">[]) {
    if (matchesStageKey(point, BUCKET_STAGE_KEYS[bucketKey])) return bucketKey;
  }
  return null;
}

export function countsSexDetail(
  counts: Pick<SummaryBucket, "female" | "male" | "other">,
  labels: SexLabels,
): string {
  return [
    `${counts.female.toLocaleString("en-IN")} ${labels.female.toLowerCase()}`,
    `${counts.male.toLocaleString("en-IN")} ${labels.male.toLowerCase()}`,
    counts.other > 0 ? `${counts.other.toLocaleString("en-IN")} ${labels.other.toLowerCase()}` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function buildCountsSummaryCards(
  points: CountsStageSexPoint[],
  labels: SexLabels,
  summaryLabels: SummaryLabels,
): CountsSummaryCard[] {
  const buckets: Record<SummaryBucketKey, SummaryBucket> = {
    fattening: emptyBucket(),
    bucks: emptyBucket(),
    breeding: emptyBucket(),
    icu: emptyBucket(),
    k0: emptyBucket(),
    k1: emptyBucket(),
    k2: emptyBucket(),
    k3: emptyBucket(),
    k4: emptyBucket(),
  };
  // Stages no card names -- a stage added on Configuration > Items & settings, or one this list
  // has not caught up with. They used to be dropped, so the cards stopped adding up to the herd.
  const unmatched = emptyBucket();

  for (const point of points) {
    const addTo = (bucketKey: SummaryBucketKey) => {
      const bucket = buckets[bucketKey];
      bucket.female += point.female;
      bucket.male += point.male;
      bucket.other += point.other;
      bucket.count += point.count;
    };

    const bucketKey = classifyStage(point);
    if (bucketKey) addTo(bucketKey);
    else {
      unmatched.female += point.female;
      unmatched.male += point.male;
      unmatched.other += point.other;
      unmatched.count += point.count;
    }
  }

  const otherCard: CountsSummaryCard[] =
    unmatched.count > 0 && summaryLabels.other
      ? [{ key: "other_stages", label: summaryLabels.other, count: unmatched.count, detail: countsSexDetail(unmatched, labels), tone: "muted" }]
      : [];

  return [
    { key: "fattening", label: summaryLabels.fattening, count: buckets.fattening.count, detail: countsSexDetail(buckets.fattening, labels), tone: "brand" },
    { key: "bucks", label: summaryLabels.bucks, count: buckets.bucks.count, detail: countsSexDetail(buckets.bucks, labels), tone: "amber" },
    { key: "breeding", label: summaryLabels.breeding, count: buckets.breeding.count, detail: countsSexDetail(buckets.breeding, labels), tone: "teal" },
    { key: "icu", label: summaryLabels.icu, count: buckets.icu.count, detail: countsSexDetail(buckets.icu, labels), tone: "muted" },
    ...(["k0", "k1", "k2", "k3", "k4"] as const).map((key) => ({
      key,
      label: summaryLabels[key],
      count: buckets[key].count,
      detail: countsSexDetail(buckets[key], labels),
      tone: "brand" as const,
    })),
    ...otherCard,
  ];
}
