package domain

var evidenceReasonLabels = map[string]string{
	EvidenceReasonBQGenderSelfConflict:          "Legacy gender self-conflict",
	EvidenceReasonBQBreedSelfConflict:           "Legacy breed self-conflict",
	EvidenceReasonLegacyChangedAfterHumanReview: "Legacy changed after human review",
	EvidenceReasonStatusMismatch:                "Lifecycle/status mismatch",
	EvidenceReasonUnregisteredSource:            "Unregistered legacy source",
}

func EvidenceReasonLabel(reason string) (string, bool) {
	label, ok := evidenceReasonLabels[reason]
	return label, ok
}
