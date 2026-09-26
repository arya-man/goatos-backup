export type GoatRow = { id: string; tag: string; pen: string; breed: string; weightKg: number; status: "active" | "sick" | "sold" };

const PENS = ["P-01", "P-02", "P-03", "Kid shed", "Isolation"];
const BREEDS = ["Sirohi", "Beetal", "Boer cross", "Osmanabadi"];
const STATUS: GoatRow["status"][] = ["active", "active", "active", "sick", "sold"];

export const GOATS: GoatRow[] = Array.from({ length: 23 }, (_, i) => ({
  id: `g${i + 1}`,
  tag: `MSG-${String(1040 + i * 7).padStart(5, "0")}`,
  pen: PENS[i % PENS.length],
  breed: BREEDS[i % BREEDS.length],
  weightKg: 18 + ((i * 37) % 29) + (i % 3) * 0.5,
  status: STATUS[i % STATUS.length],
}));
