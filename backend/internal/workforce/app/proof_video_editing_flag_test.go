package app

import "testing"

// The editor ships OFF. A bootstrap that silently enabled it would put an editing step in front
// of operators on a workflow nobody has signed off, and the client trusts this flag.
func TestProofVideoEditingShipsDisabled(t *testing.T) {
	if proofVideoEditingEnabled() {
		t.Fatal("proof_video_editing must ship false; enabling it is a reviewed maintainer change")
	}
}
