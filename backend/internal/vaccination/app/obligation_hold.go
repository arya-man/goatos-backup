package app

// obligationHoldState maps generation hold reasons to TRD obligation statuses:
// clinical blocks → waived; operational/timing holds → deferred.
func obligationHoldState(clinicalReason, operationalReason, catchUpReason string) (reason string, status string) {
	if clinicalReason != "" {
		return clinicalReason, "waived"
	}
	if operationalReason != "" {
		return operationalReason, "deferred"
	}
	if catchUpReason != "" {
		return catchUpReason, "deferred"
	}
	return "", "scheduled"
}
