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
};

type SummaryBucketKey = "fattening" | "bucks" | "breeding" | "icu" | "k0" | "k1" | "k2" | "k3" | "k4";

type SummaryBucket = {
  female: number;
  male: number;
  other: number;
  count: number;
};

const BUCKET_STAGE_KEYS: Record<SummaryBucketKey, readonly string[]> = {
  fattening: ["F2", "F2-Male", "F2-Female", "Fattening", "Fattening male", "Fattening female"],
  bucks: ["Buck", "Bucks"],
  breeding: ["Mother", "Mothers", "Milking", "M0", "Warmup", "Pregnant", "Non-Pregnant", "Non Pregnant", "Breeding female", "Breeding stock"],
  icu: ["ICU", "ICU-Kid", "ICU Kid", "ICU-Kids", "ICU Kids", "ICU-Non-Pregnant", "ICU Non Pregnant"],
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

function sexDetail(bucket: SummaryBucket, labels: SexLabels): string {
  return [
    `${bucket.female.toLocaleString("en-IN")} ${labels.female.toLowerCase()}`,
    `${bucket.male.toLocaleString("en-IN")} ${labels.male.toLowerCase()}`,
    bucket.other > 0 ? `${bucket.other.toLocaleString("en-IN")} ${labels.other.toLowerCase()}` : "",
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

  for (const point of points) {
    const addTo = (bucketKey: SummaryBucketKey) => {
      const bucket = buckets[bucketKey];
      bucket.female += point.female;
      bucket.male += point.male;
      bucket.other += point.other;
      bucket.count += point.count;
    };

    for (const bucketKey of Object.keys(BUCKET_STAGE_KEYS) as SummaryBucketKey[]) {
      if (matchesStageKey(point, BUCKET_STAGE_KEYS[bucketKey])) addTo(bucketKey);
    }
  }

  return [
    { key: "fattening", label: summaryLabels.fattening, count: buckets.fattening.count, detail: sexDetail(buckets.fattening, labels), tone: "brand" },
    { key: "bucks", label: summaryLabels.bucks, count: buckets.bucks.count, detail: sexDetail(buckets.bucks, labels), tone: "amber" },
    { key: "breeding", label: summaryLabels.breeding, count: buckets.breeding.count, detail: sexDetail(buckets.breeding, labels), tone: "teal" },
    { key: "icu", label: summaryLabels.icu, count: buckets.icu.count, detail: sexDetail(buckets.icu, labels), tone: "muted" },
    ...(["k0", "k1", "k2", "k3", "k4"] as const).map((key) => ({
      key,
      label: summaryLabels[key],
      count: buckets[key].count,
      detail: sexDetail(buckets[key], labels),
      tone: "brand" as const,
    })),
  ];
}
