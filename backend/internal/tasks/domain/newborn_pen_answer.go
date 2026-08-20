package domain

import "strings"

// The Record shed step's answer names an operational location: "<shed_id>|<partition_label>".
//
// It is the SAME key the operator's pen picker is built on, so the value an operator selects is the
// value the step stores and the value the placement resolves -- no re-encoding between the picker
// and the write, which is where a shed/pen pair silently loses its partition half.
//
// The partition half is the HUMAN label ('1', 'Part 3'), never the normalized matching key: the
// answer is stored on the action row and is shown back to the operator and the verifier, and
// rendering "Mandela 2 - 3" instead of "Mandela 2 - Part 3" is a defect this repository has shipped
// and fixed before.
const recordedPenAnswerSeparator = "|"

// ParseRecordedPenAnswer splits a Record shed answer into its shed id and partition label.
//
// An EMPTY partition half is legal and means the bare, non-partitioned shed -- the adapter then
// proves the shed genuinely has no pens before accepting it, because a partitioned shed named
// without its pen is ambiguous about where the kid physically is.
//
// Pure, so the format rule has one implementation and can be exercised without a database.
func ParseRecordedPenAnswer(answer string) (shedID string, partitionLabel string, err error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", "", ErrInvalidAnswer
	}
	shedID, partitionLabel, found := strings.Cut(answer, recordedPenAnswerSeparator)
	shedID = strings.TrimSpace(shedID)
	partitionLabel = strings.TrimSpace(partitionLabel)
	if shedID == "" {
		return "", "", ErrInvalidAnswer
	}
	// A second separator means the label itself carried one, which no real partition label does.
	// Rejecting it keeps a malformed value from resolving to a pen the operator did not choose.
	if found && strings.Contains(partitionLabel, recordedPenAnswerSeparator) {
		return "", "", ErrInvalidAnswer
	}
	return shedID, partitionLabel, nil
}

// FormatRecordedPenAnswer builds the answer value for a pen. It is the inverse of
// ParseRecordedPenAnswer and exists so tests and clients cannot hand-roll a second encoding.
func FormatRecordedPenAnswer(shedID string, partitionLabel *string) string {
	label := ""
	if partitionLabel != nil {
		label = strings.TrimSpace(*partitionLabel)
	}
	return strings.TrimSpace(shedID) + recordedPenAnswerSeparator + label
}
