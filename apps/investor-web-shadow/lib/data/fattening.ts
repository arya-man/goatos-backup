// ── Fattening mock data ──

export interface FatteningStage {
  stage: string;
  count: number;
  weight: number;
  avgWeight: number;
  value: number;
  males: number;
  females: number;
}

export interface ADGByBreed {
  breed: string;
  adg: number;
}

export interface ADGByStatus {
  status: string;
  adg: number;
}

export interface ADGByGenderBreed {
  breed: string;
  male: number;
  female: number;
}

export interface WeighingWeek {
  week: string;
  pct: number;
}

export interface LoadADG {
  load: string;
  breed: string;
  adg: number;
}

export interface LoadWeight {
  load: string;
  avgWeight: number;
}

export interface LoadWeighing {
  load: string;
  w1: number;
  w2: number;
  w3: number;
  w4: number;
  w5: number;
}

export interface WeightChange {
  goatId: string;
  load: string;
  prevWeight: number;
  currWeight: number;
  change: number;
}

// ── Overview metrics (extended) ──

export interface OverviewMetrics {
  count: number;
  weight: number;
  avgWeight: number;
  value: number;
  males: number;
  females: number;
  over30: number;
  over35: number;
}

export function getOverviewMetrics(): { total: OverviewMetrics; cbe: OverviewMetrics; cpt: OverviewMetrics } {
  return {
    total: { count: 534, weight: 11201.9, avgWeight: 20.98, value: 5600950, males: 361, females: 173, over30: 142, over35: 68 },
    cbe: { count: 312, weight: 7104.5, avgWeight: 22.77, value: 3552250, males: 218, females: 94, over30: 98, over35: 45 },
    cpt: { count: 222, weight: 4097.4, avgWeight: 18.46, value: 2048700, males: 143, females: 79, over30: 44, over35: 23 },
  };
}

// ── Demographics by stage & farm ──

export interface DemoStage {
  stage: string;
  count: number | null;
  weight: number | null;
  avgWeight: number | null;
  value: number | null;
  males: number | null;
  females: number | null;
}

export function getDemographics(farm: "all" | "cbe" | "cpt"): DemoStage[] {
  const stages = ["F1", "F2", "K0", "K1", "K2", "K3"];
  if (farm === "cbe") {
    return [
      { stage: "F1", count: 42, weight: 1260, avgWeight: 30, value: 630000, males: 28, females: 14 },
      { stage: "F2", count: 210, weight: 5628, avgWeight: 26.8, value: 2814000, males: 163, females: 47 },
      { stage: "K0", count: null, weight: null, avgWeight: null, value: null, males: null, females: null },
      { stage: "K1", count: 18, weight: 180, avgWeight: 10, value: 90000, males: 10, females: 8 },
      { stage: "K2", count: 95, weight: 807.5, avgWeight: 8.5, value: 403750, males: 44, females: 51 },
      { stage: "K3", count: null, weight: null, avgWeight: null, value: null, males: null, females: null },
    ];
  }
  if (farm === "cpt") {
    return [
      { stage: "F1", count: 30, weight: 840, avgWeight: 28, value: 420000, males: 22, females: 8 },
      { stage: "F2", count: 154, weight: 4085.7, avgWeight: 26.53, value: 2042850, males: 120, females: 34 },
      { stage: "K0", count: 12, weight: 48, avgWeight: 4, value: 24000, males: 5, females: 7 },
      { stage: "K1", count: 21, weight: 210, avgWeight: 10, value: 105000, males: 11, females: 10 },
      { stage: "K2", count: 75, weight: 680.7, avgWeight: 9.08, value: 340350, males: 34, females: 41 },
      { stage: "K3", count: null, weight: null, avgWeight: null, value: null, males: null, females: null },
    ];
  }
  // all
  return stages.map((stage) => {
    const cbeData = getDemographics("cbe").find((s) => s.stage === stage);
    const cptData = getDemographics("cpt").find((s) => s.stage === stage);
    if (!cbeData?.count && !cptData?.count) {
      return { stage, count: null, weight: null, avgWeight: null, value: null, males: null, females: null };
    }
    const count = (cbeData?.count ?? 0) + (cptData?.count ?? 0);
    const weight = (cbeData?.weight ?? 0) + (cptData?.weight ?? 0);
    return {
      stage,
      count,
      weight,
      avgWeight: count > 0 ? +(weight / count).toFixed(2) : null,
      value: (cbeData?.value ?? 0) + (cptData?.value ?? 0),
      males: (cbeData?.males ?? 0) + (cptData?.males ?? 0),
      females: (cbeData?.females ?? 0) + (cptData?.females ?? 0),
    };
  });
}

// ── Farmwise Count (legacy) ──

export function getFatteningStages(): FatteningStage[] {
  return [
    { stage: "F2", count: 364, weight: 9713.7, avgWeight: 26.79, value: 4856850, males: 283, females: 81 },
    { stage: "K2", count: 170, weight: 1488.2, avgWeight: 8.5, value: 744100, males: 78, females: 92 },
  ];
}

export function getFatteningTotals(): FatteningStage {
  const stages = getFatteningStages();
  return {
    stage: "Total",
    count: stages.reduce((s, r) => s + r.count, 0),
    weight: stages.reduce((s, r) => s + r.weight, 0),
    avgWeight: +(stages.reduce((s, r) => s + r.weight, 0) / stages.reduce((s, r) => s + r.count, 0)).toFixed(2),
    value: stages.reduce((s, r) => s + r.value, 0),
    males: stages.reduce((s, r) => s + r.males, 0),
    females: stages.reduce((s, r) => s + r.females, 0),
  };
}

// ── Kids Weighed % by farm ──

export function getKidsWeighedByFarm(farm: "total" | "cbe" | "cpt"): WeighingWeek[] {
  if (farm === "cbe") {
    return [
      { week: "W1", pct: 94 }, { week: "W2", pct: 90 }, { week: "W3", pct: 97 },
      { week: "W4", pct: 88 }, { week: "W5", pct: 92 }, { week: "W6", pct: 89 },
    ];
  }
  if (farm === "cpt") {
    return [
      { week: "W1", pct: 88 }, { week: "W2", pct: 84 }, { week: "W3", pct: 91 },
      { week: "W4", pct: 80 }, { week: "W5", pct: 86 }, { week: "W6", pct: 83 },
    ];
  }
  return [
    { week: "W1", pct: 92 }, { week: "W2", pct: 88 }, { week: "W3", pct: 95 },
    { week: "W4", pct: 85 }, { week: "W5", pct: 90 }, { week: "W6", pct: 87 },
  ];
}

// ── ADG ──

export function getOverallADG() {
  return { overall: 84.61, cbe: 88.41, cpt: 70.72 };
}

export function getADGByGenderBreed(): ADGByGenderBreed[] {
  return [
    { breed: "Beetal", male: 102, female: 85 },
    { breed: "Anantapur", male: 91, female: 78 },
    { breed: "Sojat", male: 88, female: 72 },
    { breed: "Malai", male: 76, female: 68 },
    { breed: "Osmanabadi", male: 82, female: 71 },
    { breed: "Boer", male: 95, female: 80 },
  ];
}

export function getADGByBreed(): ADGByBreed[] {
  return [
    { breed: "Beetal", adg: 94 },
    { breed: "Boer", adg: 88 },
    { breed: "Anantapur", adg: 85 },
    { breed: "Osmanabadi", adg: 77 },
    { breed: "Sojat", adg: 80 },
    { breed: "Malai", adg: 72 },
  ];
}

export function getADGByStatus(): ADGByStatus[] {
  return [
    { status: "F2", adg: 92 },
    { status: "K2", adg: 74 },
    { status: "K1", adg: 68 },
    { status: "F1", adg: 82 },
  ];
}

export function getADGByGender() {
  return [
    { name: "Male", value: 94 },
    { name: "Female", value: 77 },
  ];
}

export function getADGByBreedStatus(): { name: string; F2: number; K2: number }[] {
  return [
    { name: "Beetal", F2: 98, K2: 78 },
    { name: "Anantapur", F2: 90, K2: 72 },
    { name: "Sojat", F2: 85, K2: 68 },
    { name: "Malai", F2: 76, K2: 62 },
    { name: "Osmanabadi", F2: 82, K2: 70 },
    { name: "Boer", F2: 92, K2: 75 },
  ];
}

export function getKidsWeighedPct(): WeighingWeek[] {
  return [
    { week: "W1", pct: 92 },
    { week: "W2", pct: 88 },
    { week: "W3", pct: 95 },
    { week: "W4", pct: 85 },
    { week: "W5", pct: 90 },
    { week: "W6", pct: 87 },
  ];
}

// ── Loadwise ──

export function getLoadADGByBreed(): LoadADG[] {
  return [
    { load: "L-102", breed: "Beetal", adg: 96 },
    { load: "L-102", breed: "Anantapur", adg: 88 },
    { load: "L-103", breed: "Sojat", adg: 82 },
    { load: "L-103", breed: "Malai", adg: 74 },
    { load: "L-104", breed: "Osmanabadi", adg: 79 },
    { load: "L-104", breed: "Boer", adg: 91 },
    { load: "L-105", breed: "Beetal", adg: 93 },
    { load: "L-105", breed: "Anantapur", adg: 84 },
  ];
}

export function getAvgWeightByLoad(): LoadWeight[] {
  return [
    { load: "L-102", avgWeight: 28.5 },
    { load: "L-103", avgWeight: 24.2 },
    { load: "L-104", avgWeight: 22.8 },
    { load: "L-105", avgWeight: 26.1 },
    { load: "L-106", avgWeight: 19.7 },
  ];
}

export function getLast5WeighingsByLoad(): LoadWeighing[] {
  return [
    { load: "L-102", w1: 18.2, w2: 20.5, w3: 23.1, w4: 25.8, w5: 28.5 },
    { load: "L-103", w1: 15.0, w2: 17.4, w3: 19.8, w4: 22.0, w5: 24.2 },
    { load: "L-104", w1: 14.5, w2: 16.8, w3: 18.9, w4: 20.8, w5: 22.8 },
    { load: "L-105", w1: 16.0, w2: 18.5, w3: 21.2, w4: 23.6, w5: 26.1 },
  ];
}

export function getWeightChanges(): WeightChange[] {
  return [
    { goatId: "VG-2045", load: "L-102", prevWeight: 25.8, currWeight: 28.5, change: 2.7 },
    { goatId: "VG-2038", load: "L-102", prevWeight: 24.2, currWeight: 27.1, change: 2.9 },
    { goatId: "VG-2051", load: "L-103", prevWeight: 22.0, currWeight: 24.2, change: 2.2 },
    { goatId: "VG-2063", load: "L-103", prevWeight: 20.5, currWeight: 23.8, change: 3.3 },
    { goatId: "VG-2070", load: "L-104", prevWeight: 20.8, currWeight: 22.8, change: 2.0 },
    { goatId: "VG-2077", load: "L-104", prevWeight: 19.1, currWeight: 21.5, change: 2.4 },
    { goatId: "VG-2084", load: "L-105", prevWeight: 23.6, currWeight: 26.1, change: 2.5 },
    { goatId: "VG-2091", load: "L-105", prevWeight: 21.8, currWeight: 24.3, change: 2.5 },
    { goatId: "VG-2098", load: "L-102", prevWeight: 26.5, currWeight: 28.9, change: 2.4 },
    { goatId: "VG-2105", load: "L-103", prevWeight: 21.3, currWeight: 23.0, change: 1.7 },
  ];
}
