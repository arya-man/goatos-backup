package domain

// AggregateCountsBreakdownRowsByShed rolls duplicate compatibility rows up to exact-shed grain:
// one row per (park_id, shed_id, management_stage, breed, sex), losing any stale PartitionLabel.
//
// This is a defensive compatibility helper, not a parent-shed view. After the exact-shed cutover,
// Castro 1, Castro 2, and Castro 3 are separate sheds. If legacy partition metadata arrives beside
// an exact shed id, it is discarded so rows cannot render or key as "Castro 2 2".
func AggregateCountsBreakdownRowsByShed(rows []CountsBreakdownRow) []CountsBreakdownRow {
	type key struct {
		parkID, shedID, stage, breed, sex string
	}
	order := make([]key, 0, len(rows))
	byKey := map[key]*CountsBreakdownRow{}
	for _, row := range rows {
		k := key{
			parkID: derefOrEmpty(row.ParkID),
			shedID: derefOrEmpty(row.ShedID),
			stage:  row.ManagementStage,
			breed:  row.Breed,
			sex:    row.Sex,
		}
		agg, ok := byKey[k]
		if !ok {
			parkID, shedID := row.ParkID, row.ShedID
			agg = &CountsBreakdownRow{
				ParkID:          parkID,
				ParkLabel:       row.ParkLabel,
				ShedID:          shedID,
				ShedLabel:       row.ShedLabel,
				ManagementStage: row.ManagementStage,
				Breed:           row.Breed,
				Sex:             row.Sex,
				// Exact shed rows carry no appended partition. The shed label is already the
				// physical location name.
				OperationalLocationDisplay: row.ShedLabel,
			}
			byKey[k] = agg
			order = append(order, k)
		}
		agg.Count += row.Count
	}
	out := make([]CountsBreakdownRow, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

func derefOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
