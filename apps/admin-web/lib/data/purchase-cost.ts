// ── Purchase Cost mock data ──

export interface PurchaseCostLoad {
  loadId: number;
  vendor: string;
  breed: string;
  costPerKg: number;
  label: string;
}

export function getGoatPurchaseCosts(): PurchaseCostLoad[] {
  return [
    { loadId: 102, vendor: "Simbhu Lal", breed: "Beetal", costPerKg: 757, label: "L102 – Simbhu Lal" },
    { loadId: 103, vendor: "Yuvaan", breed: "Anantapur", costPerKg: 698, label: "L103 – Yuvaan" },
    { loadId: 104, vendor: "Breed Shoppe", breed: "Sojat", costPerKg: 645, label: "L104 – Breed Shoppe" },
    { loadId: 105, vendor: "Gokul Agronomics", breed: "Beetal", costPerKg: 720, label: "L105 – Gokul Agronomics" },
    { loadId: 106, vendor: "Simbhu Lal", breed: "Osmanabadi", costPerKg: 580, label: "L106 – Simbhu Lal" },
    { loadId: 107, vendor: "Yuvaan", breed: "Malai", costPerKg: 530, label: "L107 – Yuvaan" },
    { loadId: 108, vendor: "Breed Shoppe", breed: "Boer", costPerKg: 685, label: "L108 – Breed Shoppe" },
    { loadId: 109, vendor: "Gokul Agronomics", breed: "Anantapur", costPerKg: 620, label: "L109 – Gokul Agronomics" },
    { loadId: 110, vendor: "Simbhu Lal", breed: "Beetal", costPerKg: 710, label: "L110 – Simbhu Lal" },
    { loadId: 111, vendor: "Yuvaan", breed: "Sojat", costPerKg: 560, label: "L111 – Yuvaan" },
    { loadId: 112, vendor: "Breed Shoppe", breed: "Osmanabadi", costPerKg: 490, label: "L112 – Breed Shoppe" },
    { loadId: 113, vendor: "Gokul Agronomics", breed: "Malai", costPerKg: 475, label: "L113 – Gokul Agronomics" },
    { loadId: 114, vendor: "Simbhu Lal", breed: "Beetal", costPerKg: 740, label: "L114 – Simbhu Lal" },
    { loadId: 115, vendor: "Yuvaan", breed: "Anantapur", costPerKg: 610, label: "L115 – Yuvaan" },
    { loadId: 116, vendor: "Breed Shoppe", breed: "Sojat", costPerKg: 555, label: "L116 – Breed Shoppe" },
    { loadId: 117, vendor: "Gokul Agronomics", breed: "Boer", costPerKg: 670, label: "L117 – Gokul Agronomics" },
    { loadId: 118, vendor: "Simbhu Lal", breed: "Osmanabadi", costPerKg: 520, label: "L118 – Simbhu Lal" },
    { loadId: 119, vendor: "Yuvaan", breed: "Malai", costPerKg: 480, label: "L119 – Yuvaan" },
    { loadId: 120, vendor: "Breed Shoppe", breed: "Beetal", costPerKg: 725, label: "L120 – Breed Shoppe" },
    { loadId: 121, vendor: "Gokul Agronomics", breed: "Anantapur", costPerKg: 595, label: "L121 – Gokul Agronomics" },
    { loadId: 122, vendor: "Simbhu Lal", breed: "Sojat", costPerKg: 545, label: "L122 – Simbhu Lal" },
    { loadId: 123, vendor: "Yuvaan", breed: "Boer", costPerKg: 650, label: "L123 – Yuvaan" },
    { loadId: 124, vendor: "Breed Shoppe", breed: "Osmanabadi", costPerKg: 420, label: "L124 – Breed Shoppe" },
    { loadId: 125, vendor: "Gokul Agronomics", breed: "Malai", costPerKg: 460, label: "L125 – Gokul Agronomics" },
  ];
}

export function getSheepPurchaseCosts(): PurchaseCostLoad[] {
  return [
    { loadId: 100, vendor: "Green Fresh Farm", breed: "Nellore", costPerKg: 804, label: "L100 – Green Fresh Farm" },
    { loadId: 101, vendor: "Dr Praneeth", breed: "Deccani", costPerKg: 745, label: "L101 – Dr Praneeth" },
    { loadId: 102, vendor: "Nutriplus Foods", breed: "Nellore", costPerKg: 690, label: "L102 – Nutriplus Foods" },
    { loadId: 103, vendor: "Paddaiah", breed: "Mecheri", costPerKg: 720, label: "L103 – Paddaiah" },
    { loadId: 104, vendor: "Green Fresh Farm", breed: "Deccani", costPerKg: 655, label: "L104 – Green Fresh Farm" },
    { loadId: 105, vendor: "Dr Praneeth", breed: "Nellore", costPerKg: 710, label: "L105 – Dr Praneeth" },
    { loadId: 106, vendor: "Nutriplus Foods", breed: "Mecheri", costPerKg: 580, label: "L106 – Nutriplus Foods" },
    { loadId: 107, vendor: "Paddaiah", breed: "Deccani", costPerKg: 625, label: "L107 – Paddaiah" },
    { loadId: 108, vendor: "Green Fresh Farm", breed: "Nellore", costPerKg: 760, label: "L108 – Green Fresh Farm" },
    { loadId: 109, vendor: "Dr Praneeth", breed: "Mecheri", costPerKg: 540, label: "L109 – Dr Praneeth" },
    { loadId: 110, vendor: "Nutriplus Foods", breed: "Deccani", costPerKg: 490, label: "L110 – Nutriplus Foods" },
    { loadId: 111, vendor: "Paddaiah", breed: "Nellore", costPerKg: 680, label: "L111 – Paddaiah" },
    { loadId: 112, vendor: "Green Fresh Farm", breed: "Mecheri", costPerKg: 510, label: "L112 – Green Fresh Farm" },
    { loadId: 113, vendor: "Dr Praneeth", breed: "Deccani", costPerKg: 465, label: "L113 – Dr Praneeth" },
    { loadId: 114, vendor: "Nutriplus Foods", breed: "Nellore", costPerKg: 730, label: "L114 – Nutriplus Foods" },
    { loadId: 115, vendor: "Paddaiah", breed: "Mecheri", costPerKg: 555, label: "L115 – Paddaiah" },
    { loadId: 116, vendor: "Green Fresh Farm", breed: "Deccani", costPerKg: 420, label: "L116 – Green Fresh Farm" },
    { loadId: 117, vendor: "Dr Praneeth", breed: "Nellore", costPerKg: 695, label: "L117 – Dr Praneeth" },
    { loadId: 118, vendor: "Nutriplus Foods", breed: "Mecheri", costPerKg: 480, label: "L118 – Nutriplus Foods" },
    { loadId: 119, vendor: "Paddaiah", breed: "Deccani", costPerKg: 395, label: "L119 – Paddaiah" },
    { loadId: 120, vendor: "Green Fresh Farm", breed: "Nellore", costPerKg: 341, label: "L120 – Green Fresh Farm" },
  ];
}
