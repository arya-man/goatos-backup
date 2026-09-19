package sqlguard

import "sort"

// keywords_export.go exposes two read-only views of the validator's internals
// so sibling packages can stay consistent with it WITHOUT re-deriving the
// tokenizer or the banned-keyword list:
//
//   - reporting.TestSchemaCardsNoBannedKeywordColumns asserts that no schema
//     card column is spelled like a banned keyword (a column literally named
//     "set" or "close" could never be selected through the guard, so the card
//     would advertise a column the model cannot use).
//   - ValidateWindow (window.go) matches the required date predicates on the
//     same whole-token stream Validate sees.
//
// Neither accessor changes validator behaviour; they are pure reads.

// BannedKeywords returns the sorted, upper-case list of whole-token keywords
// Validate rejects. The slice is a fresh copy on every call.
func BannedKeywords() []string {
	out := make([]string, 0, len(bannedKeywords))
	for k := range bannedKeywords {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Token is one lexical unit of a literal-stripped SQL statement as the guard
// sees it. Text is the raw token text; IsNum / IsSym classify numbers and
// single-character symbols; everything else is an identifier or keyword.
type Token struct {
	Text  string
	IsNum bool
	IsSym bool
}

// Tokens strips string literals from sql exactly as Validate does and returns
// the resulting token stream. It returns nil (not an error) when the statement
// cannot even be literal-stripped, because a caller that needs the token stream
// will already have run Validate and been rejected.
func Tokens(sql string) []Token {
	stripped, err := stripStringLiterals(sql)
	if err != nil {
		// exception:exempt pure read; the same strip error is what Validate already returned to the caller as its *ValidationError
		return nil
	}
	raw := tokenize(stripped)
	out := make([]Token, len(raw))
	for i, t := range raw {
		out[i] = Token{Text: t.text, IsNum: t.isNum, IsSym: t.isSym}
	}
	return out
}
