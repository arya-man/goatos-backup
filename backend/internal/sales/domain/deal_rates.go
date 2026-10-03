package domain

// DealRates are one sale's per-kilogram and per-animal figures, shown on the deals ledger beside
// the totals they divide. They are derived HERE so every surface reads the same number and none
// divides its own.
//
// Every ratio ranges over the sale's LIVE lines only, and each one's numerator and denominator
// range over the same lines: a manure or feed line's value is not money for an animal, and a line
// with no recorded weight (or count) contributes to neither half of the ratio that needs it -- the
// same key set realized_price_per_kg uses on the overview, so a one-deal page agrees with it.
// A ratio with nothing to divide by is nil, never zero: a manure sale has no price per kilogram.
type DealRates struct {
	PricePerKg      *float64
	WeightPerAnimal *float64
	PricePerAnimal  *float64
}

// Rates computes the deal's DealRates.
func (d Deal) Rates() DealRates {
	var valueForKg, weightForKg float64
	var weightForEach, animalsForEach float64
	var valueForAnimal, animalsForValue float64
	for _, l := range d.lineView() {
		if !l.IsLive() {
			continue
		}
		weight, animals, value := l.WeightKg(), l.Animals(), l.SalesValue
		if weight > 0 && value > 0 {
			valueForKg += value
			weightForKg += weight
		}
		if weight > 0 && animals > 0 {
			weightForEach += weight
			animalsForEach += animals
		}
		if animals > 0 && value > 0 {
			valueForAnimal += value
			animalsForValue += animals
		}
	}
	return DealRates{
		PricePerKg:      ratio(valueForKg, weightForKg),
		WeightPerAnimal: ratio(weightForEach, animalsForEach),
		PricePerAnimal:  ratio(valueForAnimal, animalsForValue),
	}
}

func ratio(numerator, denominator float64) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := numerator / denominator
	return &v
}
