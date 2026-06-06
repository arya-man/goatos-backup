// ── Shiftings mock data ──

export interface ShiftingRecord {
  rowNum: number;
  goatId: string;
  stage: string;
  daysInStage: number;
}

export function getShiftings(): ShiftingRecord[] {
  return [
    { rowNum: 1, goatId: "VG-2045", stage: "K1", daysInStage: 38 },
    { rowNum: 2, goatId: "VG-2038", stage: "K1", daysInStage: 35 },
    { rowNum: 3, goatId: "VG-2051", stage: "K2", daysInStage: 22 },
    { rowNum: 4, goatId: "VG-2063", stage: "K1", daysInStage: 41 },
    { rowNum: 5, goatId: "VG-2070", stage: "K2", daysInStage: 18 },
    { rowNum: 6, goatId: "VG-2077", stage: "K1", daysInStage: 33 },
    { rowNum: 7, goatId: "VG-2084", stage: "K2", daysInStage: 14 },
    { rowNum: 8, goatId: "VG-2091", stage: "K2", daysInStage: 27 },
    { rowNum: 9, goatId: "VG-2098", stage: "K1", daysInStage: 39 },
    { rowNum: 10, goatId: "VG-2105", stage: "K2", daysInStage: 12 },
    { rowNum: 11, goatId: "VG-2112", stage: "K1", daysInStage: 29 },
    { rowNum: 12, goatId: "VG-2119", stage: "K2", daysInStage: 8 },
    { rowNum: 13, goatId: "VG-2126", stage: "K1", daysInStage: 36 },
    { rowNum: 14, goatId: "VG-2133", stage: "K2", daysInStage: 20 },
    { rowNum: 15, goatId: "VG-2140", stage: "K1", daysInStage: 31 },
    { rowNum: 16, goatId: "VG-2147", stage: "K2", daysInStage: 15 },
    { rowNum: 17, goatId: "VG-2154", stage: "K1", daysInStage: 28 },
    { rowNum: 18, goatId: "VG-2161", stage: "K2", daysInStage: 10 },
    { rowNum: 19, goatId: "VG-2168", stage: "K1", daysInStage: 40 },
    { rowNum: 20, goatId: "VG-2175", stage: "K2", daysInStage: 24 },
    { rowNum: 21, goatId: "VG-2182", stage: "K1", daysInStage: 34 },
    { rowNum: 22, goatId: "VG-2189", stage: "K2", daysInStage: 16 },
    { rowNum: 23, goatId: "VG-2196", stage: "K1", daysInStage: 37 },
    { rowNum: 24, goatId: "VG-2203", stage: "K2", daysInStage: 11 },
    { rowNum: 25, goatId: "VG-2210", stage: "K1", daysInStage: 26 },
    { rowNum: 26, goatId: "VG-2217", stage: "K3", daysInStage: 18 },
    { rowNum: 27, goatId: "VG-2224", stage: "K3", daysInStage: 25 },
    { rowNum: 28, goatId: "VG-2231", stage: "K3", daysInStage: 12 },
    { rowNum: 29, goatId: "VG-2238", stage: "K3", daysInStage: 30 },
    { rowNum: 30, goatId: "VG-2245", stage: "K3", daysInStage: 9 },
  ];
}
