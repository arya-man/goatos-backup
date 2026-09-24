package http

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// THE IMPORT'S FINGERPRINT MUST DESCRIBE THE SHEET THAT WAS UPLOADED.
//
// It was `configFingerprint(importRegisterSheetCommand, []byte(class))` -- the animal class and
// nothing else -- so every upload for one diagnosis type produced the SAME fingerprint. The
// authoring ledger does refuse a same-key/different-fingerprint replay, but it was never reached:
// two different sheets under one key looked identical to it and the second was returned as an
// idempotent REPLAY.
//
// That is silent and it lies in the worst direction. The screen mints one key per chosen file and
// clears it only ON SUCCESS, so a write that COMMITTED but whose response never reached the
// browser leaves the key live; the author fixes the sheet, uploads again, and is told the import
// succeeded while the corrected register was never applied.
//
// Fingerprinting the PARSED DOCUMENT rather than the raw bytes is deliberate: a workbook re-saved
// by Excel differs byte for byte while describing the same register, and that is a replay, not a
// new request.
func TestImportFingerprintDescribesTheUploadedSheet(t *testing.T) {
	one := &diagnosis.AuthoredRegister{
		RegisterVersion: "1",
		AppliesClass:    []string{"adult"},
		Questions:       []diagnosis.Question{{ID: "temp", Title: "Temperature"}},
	}
	two := &diagnosis.AuthoredRegister{
		RegisterVersion: "1",
		AppliesClass:    []string{"adult"},
		Questions:       []diagnosis.Question{{ID: "temp", Title: "Temperature"}, {ID: "udder", Title: "Udder"}},
	}

	sameSheet := importSheetFingerprint("adult", one)
	if sameSheet != importSheetFingerprint("adult", one) {
		t.Fatal("the same sheet fingerprinted twice must match, or a network retry conflicts with itself")
	}
	if got := importSheetFingerprint("adult", two); got == sameSheet {
		t.Fatalf("a DIFFERENT sheet for the same type fingerprinted identically (%s); the ledger would replay the first and silently discard the correction", got)
	}
	// The class still separates types: the same document imported against two registers is two
	// different requests.
	if importSheetFingerprint("kid_milk", one) == sameSheet {
		t.Fatal("the animal class must stay part of the fingerprint")
	}
}
