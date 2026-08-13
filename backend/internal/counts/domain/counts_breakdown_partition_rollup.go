package domain

// AggregateCountsBreakdownRowsByShed rolls partition-grain CountsBreakdownRow rows (the default
// grain GetCountsBreakdown returns) up to the parent-shed grain: one row per
// (park_id, shed_id, management_stage, breed, sex), losing PartitionLabel and
// OperationalLocationDisplay's partition suffix.
//
// This is the explicit "parent aggregate" mode called out by the partition-aware breakdown
// contract: partition rows are the default detail grain, but a caller that wants Castro's total
// rather than Castro 1 + Castro 2 + Castro 3 as separate lines gets it by re-rolling this
// function over the SAME rows the partitioned query already returned -- never a second query --
// so the parent aggregate is definitionally the sum of its partitions and can never drift from
// them.
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
				// Parent aggregate rows carry no partition -- OperationalLocationDisplay
				// collapses to the bare shed label, matching oploc's non-partitioned rendering.
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
