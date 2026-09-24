package kmetrics

import "context"

var vaccinationGuardRejected = newCounter("kernel.vaccination_generation.guard_rejected", "{dose}", "Vaccination doses generation proposed that the obligation write guard refused (age floor or purpose applicability).")

// RecordVaccinationGuardRejected counts doses the persistence write guard refused during a
// generation pass. Any non-zero value means generation and persistence disagree about a dose.
func RecordVaccinationGuardRejected(ctx context.Context, rejected int) {
	if rejected > 0 {
		addCounter(ctx, vaccinationGuardRejected, int64(rejected))
	}
}
