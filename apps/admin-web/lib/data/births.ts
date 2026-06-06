// ── Births mock data ──

export interface BirthsKPI {
  totalBirths: number;
  avgKidsPerMother: number;
}

export interface BreedEfficiency {
  breed: string;
  pct: number;
}

export interface DailyBirthCount {
  date: string;
  count: number;
}

export interface KiddingFrequency {
  breed: string;
  months: number;
}

export interface LitterSize {
  breed: string;
  size: number;
}

export function getBirthsKPI(): BirthsKPI {
  return { totalBirths: 803, avgKidsPerMother: 1.25 };
}

export function getKiddingEfficiency(): BreedEfficiency[] {
  return [
    { breed: "Anantapur", pct: 72 },
    { breed: "Beetal", pct: 65 },
    { breed: "Sojat", pct: 58 },
    { breed: "Malai", pct: 45 },
    { breed: "Osmanabadi", pct: 40 },
    { breed: "Boer", pct: 0 },
  ];
}

export function getBirthCountLast10Days(): DailyBirthCount[] {
  const today = new Date();
  const counts = [12, 7, 9, 14, 6, 11, 8, 15, 5, 10];
  return counts.map((count, i) => {
    const d = new Date(today);
    d.setDate(d.getDate() - (9 - i));
    const label = `${d.getDate()}/${d.getMonth() + 1}`;
    return { date: label, count };
  });
}

export function getKiddingFrequency(): KiddingFrequency[] {
  return [
    { breed: "Anantapur", months: 7.2 },
    { breed: "Beetal", months: 8.1 },
    { breed: "Sojat", months: 7.8 },
    { breed: "Malai", months: 9.5 },
    { breed: "Osmanabadi", months: 8.8 },
  ];
}

export function getLitterSize(): LitterSize[] {
  return [
    { breed: "Anantapur", size: 1.3 },
    { breed: "Beetal", size: 1.4 },
    { breed: "Sojat", size: 1.1 },
    { breed: "Malai", size: 1.2 },
    { breed: "Osmanabadi", size: 1.15 },
    { breed: "Boer", size: 1.0 },
  ];
}
