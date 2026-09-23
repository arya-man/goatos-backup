package app

// absenceexpr.go: a tiny SQL expression reader, and the three-point test that
// decides whether a predicate is asking a column whether it is BLANK.
//
// WHY THIS EXISTS AT ALL. The first version of the emptiness gate matched
// SHAPES: `IS [NOT] NULL` or `= ''` written next to the column, stepping over a
// `coalesce(...)` wrapper on the way. A review round drove seven ordinary
// re-spellings straight through it — `nullif(x,'') IS NULL`,
// `coalesce(x,'none') = 'none'`, `x::text IS NULL`, `length(x) = 0`,
// `upper(x) IS NULL`, `x || '' = ''`, `coalesce(x) IS NULL` — and every one of
// them is a phrasing a model reaches for FIRST when a NULL test has just been
// refused. Enumerating spellings is a losing game: the list is the attack
// surface, and the refusal message itself is a hint about which spelling to try
// next.
//
// So the gate no longer asks what the predicate LOOKS like. It asks what the
// predicate DOES, by reading it as an expression and evaluating it three times
// with the column standing in turn for
//
//	NULL, the empty string, and one ordinary non-blank value,
//
// and refusing when the predicate is TRUE for a blank column and FALSE for a
// filled one. That is the definition of "tests this column for emptiness",
// written once, in the only terms that cannot be re-spelled: every wrapper that
// preserves blankness is seen through by evaluating it, not by listing it.
//
// The same three-point test is what finally lets the LEGITIMATE inverse
// through. `backup_label IS NOT NULL` is TRUE for a filled column, so it is not
// a blankness test at all; it is the correct way to ask which roles DO have a
// named backup, and the shape-matching gate was wrong to refuse it.
//
// Anything the reader cannot evaluate is not quietly allowed. A guarded column
// that reaches a boolean predicate through a function this file does not model
// is LAUNDERED: the reader cannot see whether blankness survived the wrapper,
// and a wrapper it cannot see through, applied to this specific column, inside
// a predicate, is exactly the shape the defect takes. Those are refused, and
// the refusal says so.

import (
	"strconv"
	"strings"
)

// avKind is the little value domain the three-point evaluation runs in.
type avKind int

const (
	avNull avKind = iota
	avText
	avNum
	avBool
	// avOpaque is "this reader cannot say" — an unmodelled function, a column
	// other than the one under test, an aggregate. It is contagious.
	avOpaque
)

type aval struct {
	kind avKind
	text string
	num  float64
	b    bool
	// derived marks a value that the guarded column flowed into, so an
	// unmodelled wrapper can be reported as laundering THIS column rather than
	// as ordinary opacity somewhere else in the clause.
	derived bool
}

func textVal(s string, derived bool) aval { return aval{kind: avText, text: s, derived: derived} }
func numVal(n float64, derived bool) aval { return aval{kind: avNum, num: n, derived: derived} }
func boolVal(b bool, derived bool) aval   { return aval{kind: avBool, b: b, derived: derived} }
func nullVal(derived bool) aval           { return aval{kind: avNull, derived: derived} }
func opaqueVal(derived bool) aval         { return aval{kind: avOpaque, derived: derived} }

// blankProbePresent is the "ordinary filled value" leg of the three-point test.
// It is deliberately a string no statement can contain: the lexer only ever
// hands this file literals written in the SQL, and a comparison against a value
// that cannot be written is FALSE for the right reason — the predicate
// distinguishes blank from filled — rather than by accidentally colliding with
// whatever literal the author happened to choose.
const blankProbePresent = "\x01filled\x01"

// ---------------------------------------------------------------------------
// expression nodes
// ---------------------------------------------------------------------------

type exprKind int

const (
	exLiteral exprKind = iota // a single-quoted string literal
	exNumber
	exColumn
	exNullKeyword
	exStar
	exCall
	exCast
	exUnary
	exBinary
	exCompare
	exIsNull
	exIsDistinct
	exIn
	exLike
	exBetween
	exNot
	exAnd
	exOr
	exCase
)

type exprNode struct {
	kind exprKind
	text string // literal body, column name, function name, operator
	neg  bool   // IS NOT NULL, NOT IN, NOT LIKE, IS NOT DISTINCT FROM
	num  float64
	args []*exprNode
}

// ---------------------------------------------------------------------------
// parser
// ---------------------------------------------------------------------------

type exprParser struct {
	toks []sqlToken
	i    int
	bad  bool
}

// parseSQLExpression reads one expression out of a slice of significant tokens.
// ok is false when the slice is not an expression this reader understands; the
// caller decides what to do about that, and for the emptiness gate "cannot read
// it" is not the same as "it is fine".
func parseSQLExpression(toks []sqlToken) (*exprNode, bool) {
	p := &exprParser{toks: toks}
	n := p.parseOr()
	if p.bad || n == nil || p.i != len(p.toks) {
		return n, false
	}
	return n, true
}

func (p *exprParser) peek() (sqlToken, bool) {
	if p.i >= len(p.toks) {
		return sqlToken{}, false
	}
	return p.toks[p.i], true
}

func (p *exprParser) peekAt(off int) (sqlToken, bool) {
	if p.i+off >= len(p.toks) {
		return sqlToken{}, false
	}
	return p.toks[p.i+off], true
}

func (p *exprParser) atWord(w string) bool {
	t, ok := p.peek()
	return ok && t.kind == tokIdent && strings.EqualFold(t.text, w)
}

func (p *exprParser) atOther(s string) bool {
	t, ok := p.peek()
	return ok && t.kind == tokOther && t.text == s
}

func (p *exprParser) take() sqlToken {
	t := p.toks[p.i]
	p.i++
	return t
}

func (p *exprParser) eatWord(w string) bool {
	if p.atWord(w) {
		p.i++
		return true
	}
	return false
}

func (p *exprParser) eatOther(s string) bool {
	if p.atOther(s) {
		p.i++
		return true
	}
	return false
}

func (p *exprParser) fail() *exprNode {
	p.bad = true
	return nil
}

func (p *exprParser) parseOr() *exprNode {
	left := p.parseAnd()
	for !p.bad && p.atWord("or") {
		p.i++
		right := p.parseAnd()
		left = &exprNode{kind: exOr, args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseAnd() *exprNode {
	left := p.parseNot()
	for !p.bad && p.atWord("and") {
		p.i++
		right := p.parseNot()
		left = &exprNode{kind: exAnd, args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseNot() *exprNode {
	if p.atWord("not") {
		p.i++
		return &exprNode{kind: exNot, args: []*exprNode{p.parseNot()}}
	}
	return p.parseCompare()
}

// comparisonOperator merges the two-byte operators the lexer emits one byte at
// a time (`<=`, `>=`) by checking that the bytes are genuinely adjacent in the
// original statement, so `< =` written with a space is not silently accepted as
// something Postgres would reject.
func (p *exprParser) comparisonOperator() (string, int, bool) {
	t, ok := p.peek()
	if !ok || t.kind != tokOther {
		return "", 0, false
	}
	switch t.text {
	case "<>", "!=":
		return t.text, 1, true
	case "=":
		return "=", 1, true
	case "<", ">":
		if nx, ok := p.peekAt(1); ok && nx.kind == tokOther && nx.text == "=" && nx.start == t.end {
			return t.text + "=", 2, true
		}
		return t.text, 1, true
	}
	return "", 0, false
}

func (p *exprParser) parseCompare() *exprNode {
	left := p.parseConcat()
	if p.bad {
		return left
	}
	if p.atWord("is") {
		p.i++
		neg := p.eatWord("not")
		switch {
		case p.eatWord("null"):
			return &exprNode{kind: exIsNull, neg: neg, args: []*exprNode{left}}
		case p.atWord("distinct"):
			p.i++
			if !p.eatWord("from") {
				return p.fail()
			}
			right := p.parseConcat()
			return &exprNode{kind: exIsDistinct, neg: neg, args: []*exprNode{left, right}}
		case p.atWord("true"), p.atWord("false"):
			w := p.take()
			// `x IS TRUE` is `x` itself; `x IS FALSE` is its negation.
			out := left
			if !strings.EqualFold(w.text, "true") {
				out = &exprNode{kind: exNot, args: []*exprNode{left}}
			}
			if neg {
				out = &exprNode{kind: exNot, args: []*exprNode{out}}
			}
			return out
		default:
			return p.fail()
		}
	}
	neg := false
	if p.atWord("not") {
		if nx, ok := p.peekAt(1); ok && nx.kind == tokIdent &&
			(strings.EqualFold(nx.text, "in") || strings.EqualFold(nx.text, "like") ||
				strings.EqualFold(nx.text, "ilike") || strings.EqualFold(nx.text, "between")) {
			p.i++
			neg = true
		}
	}
	switch {
	case p.atWord("in"):
		p.i++
		if !p.eatOther("(") {
			return p.fail()
		}
		args := []*exprNode{left}
		for {
			args = append(args, p.parseOr())
			if p.bad {
				return nil
			}
			if p.eatOther(",") {
				continue
			}
			break
		}
		if !p.eatOther(")") {
			return p.fail()
		}
		return &exprNode{kind: exIn, neg: neg, args: args}
	case p.atWord("like"), p.atWord("ilike"):
		w := p.take()
		right := p.parseConcat()
		return &exprNode{kind: exLike, neg: neg, text: strings.ToLower(w.text), args: []*exprNode{left, right}}
	case p.atWord("between"):
		p.i++
		lo := p.parseConcat()
		if !p.eatWord("and") {
			return p.fail()
		}
		hi := p.parseConcat()
		return &exprNode{kind: exBetween, neg: neg, args: []*exprNode{left, lo, hi}}
	}
	if neg {
		return p.fail()
	}
	if op, width, ok := p.comparisonOperator(); ok {
		p.i += width
		right := p.parseConcat()
		return &exprNode{kind: exCompare, text: op, args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseConcat() *exprNode {
	left := p.parseAdditive()
	for !p.bad {
		t, ok := p.peek()
		if !ok || t.kind != tokOther || t.text != "|" {
			break
		}
		nx, ok := p.peekAt(1)
		if !ok || nx.kind != tokOther || nx.text != "|" || nx.start != t.end {
			break
		}
		p.i += 2
		right := p.parseAdditive()
		left = &exprNode{kind: exBinary, text: "||", args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseAdditive() *exprNode {
	left := p.parseMultiplicative()
	for !p.bad {
		t, ok := p.peek()
		if !ok || t.kind != tokOther || (t.text != "+" && t.text != "-") {
			break
		}
		p.i++
		right := p.parseMultiplicative()
		left = &exprNode{kind: exBinary, text: t.text, args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseMultiplicative() *exprNode {
	left := p.parseUnary()
	for !p.bad {
		t, ok := p.peek()
		if !ok || t.kind != tokOther || (t.text != "*" && t.text != "/" && t.text != "%") {
			break
		}
		p.i++
		right := p.parseUnary()
		left = &exprNode{kind: exBinary, text: t.text, args: []*exprNode{left, right}}
	}
	return left
}

func (p *exprParser) parseUnary() *exprNode {
	if t, ok := p.peek(); ok && t.kind == tokOther && (t.text == "-" || t.text == "+") {
		p.i++
		return &exprNode{kind: exUnary, text: t.text, args: []*exprNode{p.parseUnary()}}
	}
	return p.parsePostfix()
}

func (p *exprParser) parsePostfix() *exprNode {
	n := p.parsePrimary()
	for !p.bad {
		t, ok := p.peek()
		if !ok || t.kind != tokOther || t.text != ":" {
			break
		}
		nx, ok := p.peekAt(1)
		if !ok || nx.kind != tokOther || nx.text != ":" || nx.start != t.end {
			break
		}
		p.i += 2
		ty, ok := p.peek()
		if !ok || ty.kind != tokIdent {
			return p.fail()
		}
		p.i++
		// `numeric(10,2)` and friends carry a size the cast does not change.
		if p.atOther("(") {
			depth := 0
			for p.i < len(p.toks) {
				if p.atOther("(") {
					depth++
				} else if p.atOther(")") {
					depth--
					if depth == 0 {
						p.i++
						break
					}
				}
				p.i++
			}
		}
		n = &exprNode{kind: exCast, text: strings.ToLower(ty.text), args: []*exprNode{n}}
	}
	return n
}

// sqlKeywordsThatCannotStartAnExpression keeps the primary parser from reading
// a clause keyword as a bare column name, which would make a truncated atom
// parse "successfully" as something it is not.
var sqlKeywordsThatCannotStartAnExpression = map[string]bool{
	"and": true, "or": true, "not": true, "is": true, "in": true, "like": true,
	"ilike": true, "between": true, "from": true, "where": true, "select": true,
	"group": true, "order": true, "by": true, "having": true, "limit": true,
	"offset": true, "as": true, "when": true, "then": true, "else": true,
	"end": true, "on": true, "join": true, "union": true, "distinct": true,
	"filter": true, "over": true, "partition": true,
}

func (p *exprParser) parsePrimary() *exprNode {
	t, ok := p.peek()
	if !ok {
		return p.fail()
	}
	switch t.kind {
	case tokString:
		p.i++
		return &exprNode{kind: exLiteral, text: sqlLiteralBody(t.text)}
	case tokIdent:
		low := strings.ToLower(t.text)
		if low == "null" {
			p.i++
			return &exprNode{kind: exNullKeyword}
		}
		if low == "case" {
			return p.parseCase()
		}
		if sqlKeywordsThatCannotStartAnExpression[low] {
			return p.fail()
		}
		p.i++
		// qualified name: a.b.c — the last part is the column.
		name := t.text
		for p.atOther(".") {
			p.i++
			nx, ok := p.peek()
			if !ok || nx.kind != tokIdent {
				return p.fail()
			}
			p.i++
			name = nx.text
		}
		if p.atOther("(") {
			p.i++
			call := &exprNode{kind: exCall, text: strings.ToLower(name)}
			_ = p.eatWord("distinct")
			if !p.atOther(")") {
				for {
					call.args = append(call.args, p.parseOr())
					if p.bad {
						return nil
					}
					if p.eatOther(",") {
						continue
					}
					break
				}
			}
			if !p.eatOther(")") {
				return p.fail()
			}
			return call
		}
		return &exprNode{kind: exColumn, text: name}
	case tokOther:
		switch t.text {
		case "(":
			p.i++
			inner := p.parseOr()
			if p.bad {
				return nil
			}
			if !p.eatOther(")") {
				return p.fail()
			}
			return inner
		case "*":
			p.i++
			return &exprNode{kind: exStar}
		}
		if n, ok := p.parseNumber(); ok {
			return n
		}
	}
	return p.fail()
}

// parseNumber reassembles a numeric literal. The lexer refuses to start an
// identifier with a digit, so `100` arrives as three adjacent single-byte
// tokens; adjacency in the original bytes is what makes them one number.
func (p *exprParser) parseNumber() (*exprNode, bool) {
	start := p.i
	var sb strings.Builder
	prevEnd := -1
	for p.i < len(p.toks) {
		t := p.toks[p.i]
		body := t.text
		isDigit := t.kind == tokOther && len(body) == 1 && body[0] >= '0' && body[0] <= '9'
		isDot := t.kind == tokOther && body == "."
		isIdentDigits := t.kind == tokIdent && allDigits(body)
		if !isDigit && !isDot && !isIdentDigits {
			break
		}
		if prevEnd >= 0 && t.start != prevEnd {
			break
		}
		if isDot && sb.Len() == 0 {
			break
		}
		sb.WriteString(body)
		prevEnd = t.end
		p.i++
	}
	if sb.Len() == 0 {
		p.i = start
		return nil, false
	}
	f, err := strconv.ParseFloat(sb.String(), 64)
	if err != nil {
		p.i = start
		return nil, false
	}
	return &exprNode{kind: exNumber, num: f}, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (p *exprParser) parseCase() *exprNode {
	if !p.eatWord("case") {
		return p.fail()
	}
	n := &exprNode{kind: exCase}
	for p.atWord("when") {
		p.i++
		cond := p.parseOr()
		if !p.eatWord("then") {
			return p.fail()
		}
		then := p.parseOr()
		n.args = append(n.args, cond, then)
		if p.bad {
			return nil
		}
	}
	if p.eatWord("else") {
		n.args = append(n.args, nil, p.parseOr())
	}
	if !p.eatWord("end") {
		return p.fail()
	}
	return n
}

// sqlLiteralBody strips the surrounding quotes and unescapes doubled quotes.
func sqlLiteralBody(tok string) string {
	s := tok
	if strings.HasPrefix(s, "'") {
		s = s[1:]
	}
	if strings.HasSuffix(s, "'") && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return strings.ReplaceAll(s, "''", "'")
}

// ---------------------------------------------------------------------------
// three-point evaluation
// ---------------------------------------------------------------------------

type blankEvaluator struct {
	// guarded is the set of column names (lower case) standing in for the
	// probe value; every other column is opaque.
	guarded map[string]bool
	probe   aval
	// laundered records that a value derived from a guarded column passed
	// through something this reader cannot model.
	laundered bool
}

func (e *blankEvaluator) eval(n *exprNode) aval {
	if n == nil {
		return nullVal(false)
	}
	switch n.kind {
	case exLiteral:
		return textVal(n.text, false)
	case exNumber:
		return numVal(n.num, false)
	case exNullKeyword:
		return nullVal(false)
	case exStar:
		return opaqueVal(false)
	case exColumn:
		if e.guarded[strings.ToLower(n.text)] {
			return e.probe
		}
		return opaqueVal(false)
	case exCast:
		return e.evalCast(n)
	case exCall:
		return e.evalCall(n)
	case exUnary:
		v := e.eval(n.args[0])
		if v.kind == avNum && n.text == "-" {
			return numVal(-v.num, v.derived)
		}
		if v.kind == avNum {
			return v
		}
		return e.launder(v)
	case exBinary:
		return e.evalBinary(n)
	case exCompare:
		return e.evalCompare(n)
	case exIsNull:
		v := e.eval(n.args[0])
		if v.kind == avOpaque {
			return e.launder(v)
		}
		isNull := v.kind == avNull
		if n.neg {
			isNull = !isNull
		}
		return boolVal(isNull, v.derived)
	case exIsDistinct:
		l, r := e.eval(n.args[0]), e.eval(n.args[1])
		if l.kind == avOpaque || r.kind == avOpaque {
			return e.launder(mergeDerived(l, r))
		}
		distinct := !avNotDistinct(l, r)
		if n.neg {
			distinct = !distinct
		}
		return boolVal(distinct, l.derived || r.derived)
	case exNot:
		v := e.eval(n.args[0])
		switch v.kind {
		case avBool:
			return boolVal(!v.b, v.derived)
		case avNull:
			return nullVal(v.derived)
		}
		return e.launder(v)
	case exAnd, exOr:
		l, r := e.eval(n.args[0]), e.eval(n.args[1])
		return e.evalJunction(n.kind, l, r)
	case exIn:
		return e.evalIn(n)
	case exLike:
		return e.evalLike(n)
	case exBetween:
		return e.evalBetween(n)
	case exCase:
		return e.evalCase(n)
	}
	return e.launder(opaqueVal(false))
}

// launder marks and returns opacity. When the opaque value carries a guarded
// column's contribution, the gate treats the whole predicate as unreadable
// ABOUT THAT COLUMN, which is a refusal rather than a pass.
func (e *blankEvaluator) launder(v aval) aval {
	if v.derived {
		e.laundered = true
	}
	return opaqueVal(v.derived)
}

func mergeDerived(vs ...aval) aval {
	out := opaqueVal(false)
	for _, v := range vs {
		if v.derived {
			out.derived = true
		}
	}
	return out
}

// avNotDistinct is the NULL-aware equality `IS NOT DISTINCT FROM` uses, where
// two NULLs are the same value rather than an unknown comparison.
func avNotDistinct(l, r aval) bool {
	if l.kind == avNull || r.kind == avNull {
		return l.kind == avNull && r.kind == avNull
	}
	return avEqual(l, r)
}

func (e *blankEvaluator) evalJunction(k exprKind, l, r aval) aval {
	truthy := func(v aval) (bool, bool) { // value, known
		if v.kind == avBool {
			return v.b, true
		}
		return false, false
	}
	lb, lk := truthy(l)
	rb, rk := truthy(r)
	derived := l.derived || r.derived
	if k == exAnd {
		if (lk && !lb) || (rk && !rb) {
			return boolVal(false, derived)
		}
		if lk && rk {
			return boolVal(true, derived)
		}
	} else {
		if (lk && lb) || (rk && rb) {
			return boolVal(true, derived)
		}
		if lk && rk {
			return boolVal(false, derived)
		}
	}
	if l.kind == avOpaque || r.kind == avOpaque {
		return e.launder(mergeDerived(l, r))
	}
	return nullVal(derived)
}

func (e *blankEvaluator) evalCast(n *exprNode) aval {
	v := e.eval(n.args[0])
	if v.kind == avOpaque {
		return e.launder(v)
	}
	if v.kind == avNull {
		return v
	}
	switch n.text {
	case "text", "varchar", "char", "bpchar", "citext", "name":
		return textVal(avText2Text(v), v.derived)
	case "int", "int2", "int4", "int8", "integer", "bigint", "smallint", "numeric", "decimal", "float", "float4", "float8", "real", "double":
		if v.kind == avNum {
			return v
		}
		if v.kind == avText {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v.text), 64); err == nil {
				return numVal(f, v.derived)
			}
		}
		// A cast that would raise rather than return is not something this
		// reader models; opacity, not a guess.
		return e.launder(opaqueVal(v.derived))
	}
	return e.launder(opaqueVal(v.derived))
}

func avText2Text(v aval) string {
	switch v.kind {
	case avText:
		return v.text
	case avNum:
		return strconv.FormatFloat(v.num, 'f', -1, 64)
	case avBool:
		return strconv.FormatBool(v.b)
	}
	return ""
}

func (e *blankEvaluator) evalCall(n *exprNode) aval {
	name := n.text
	args := make([]aval, 0, len(n.args))
	derived := false
	for _, a := range n.args {
		v := e.eval(a)
		if v.derived {
			derived = true
		}
		args = append(args, v)
	}
	switch name {
	case "coalesce":
		for _, v := range args {
			switch v.kind {
			case avNull:
				continue
			case avOpaque:
				return e.launder(opaqueVal(derived))
			default:
				return withDerived(v, derived)
			}
		}
		return nullVal(derived)
	case "nullif":
		if len(args) != 2 {
			return e.launder(opaqueVal(derived))
		}
		if args[0].kind == avOpaque || args[1].kind == avOpaque {
			return e.launder(opaqueVal(derived))
		}
		if args[0].kind == avNull {
			return nullVal(derived)
		}
		if args[1].kind != avNull && avEqual(args[0], args[1]) {
			return nullVal(derived)
		}
		return withDerived(args[0], derived)
	case "upper", "lower", "initcap", "trim", "btrim", "ltrim", "rtrim":
		if len(args) == 0 {
			return e.launder(opaqueVal(derived))
		}
		v := args[0]
		if v.kind == avNull {
			return nullVal(derived)
		}
		if v.kind != avText {
			return e.launder(opaqueVal(derived))
		}
		s := v.text
		switch name {
		case "upper":
			s = strings.ToUpper(s)
		case "lower":
			s = strings.ToLower(s)
		case "initcap":
			s = strings.Title(strings.ToLower(s)) //nolint:staticcheck // probe values only
		case "trim", "btrim":
			s = trimWith(s, args, strings.Trim)
		case "ltrim":
			s = trimWith(s, args, strings.TrimLeft)
		case "rtrim":
			s = trimWith(s, args, strings.TrimRight)
		}
		return textVal(s, derived)
	case "length", "char_length", "character_length", "octet_length":
		if len(args) == 0 {
			return e.launder(opaqueVal(derived))
		}
		v := args[0]
		if v.kind == avNull {
			return nullVal(derived)
		}
		if v.kind != avText {
			return e.launder(opaqueVal(derived))
		}
		return numVal(float64(len(v.text)), derived)
	case "concat":
		var sb strings.Builder
		for _, v := range args {
			if v.kind == avOpaque {
				return e.launder(opaqueVal(derived))
			}
			if v.kind == avNull {
				continue
			}
			sb.WriteString(avText2Text(v))
		}
		return textVal(sb.String(), derived)
	case "concat_ws":
		if len(args) == 0 {
			return e.launder(opaqueVal(derived))
		}
		sep := avText2Text(args[0])
		var parts []string
		for _, v := range args[1:] {
			if v.kind == avOpaque {
				return e.launder(opaqueVal(derived))
			}
			if v.kind == avNull {
				continue
			}
			parts = append(parts, avText2Text(v))
		}
		return textVal(strings.Join(parts, sep), derived)
	}
	return e.launder(opaqueVal(derived))
}

func trimWith(s string, args []aval, f func(string, string) string) string {
	cut := " \t\n\r"
	if len(args) >= 2 && args[1].kind == avText {
		cut = args[1].text
	}
	return f(s, cut)
}

func withDerived(v aval, derived bool) aval {
	if derived {
		v.derived = true
	}
	return v
}

func (e *blankEvaluator) evalBinary(n *exprNode) aval {
	l, r := e.eval(n.args[0]), e.eval(n.args[1])
	derived := l.derived || r.derived
	if l.kind == avOpaque || r.kind == avOpaque {
		return e.launder(mergeDerived(l, r))
	}
	if n.text == "||" {
		if l.kind == avNull || r.kind == avNull {
			return nullVal(derived)
		}
		return textVal(avText2Text(l)+avText2Text(r), derived)
	}
	if l.kind == avNull || r.kind == avNull {
		return nullVal(derived)
	}
	if l.kind != avNum || r.kind != avNum {
		return e.launder(opaqueVal(derived))
	}
	switch n.text {
	case "+":
		return numVal(l.num+r.num, derived)
	case "-":
		return numVal(l.num-r.num, derived)
	case "*":
		return numVal(l.num*r.num, derived)
	case "/":
		if r.num == 0 {
			return e.launder(opaqueVal(derived))
		}
		return numVal(l.num/r.num, derived)
	}
	return e.launder(opaqueVal(derived))
}

func (e *blankEvaluator) evalCompare(n *exprNode) aval {
	l, r := e.eval(n.args[0]), e.eval(n.args[1])
	derived := l.derived || r.derived
	if l.kind == avOpaque || r.kind == avOpaque {
		return e.launder(mergeDerived(l, r))
	}
	if l.kind == avNull || r.kind == avNull {
		return nullVal(derived)
	}
	switch n.text {
	case "=":
		return boolVal(avEqual(l, r), derived)
	case "<>", "!=":
		return boolVal(!avEqual(l, r), derived)
	}
	c, ok := avOrder(l, r)
	if !ok {
		return e.launder(opaqueVal(derived))
	}
	switch n.text {
	case "<":
		return boolVal(c < 0, derived)
	case "<=":
		return boolVal(c <= 0, derived)
	case ">":
		return boolVal(c > 0, derived)
	case ">=":
		return boolVal(c >= 0, derived)
	}
	return e.launder(opaqueVal(derived))
}

func avEqual(l, r aval) bool {
	if l.kind == avNum && r.kind == avNum {
		return l.num == r.num
	}
	if l.kind == avBool && r.kind == avBool {
		return l.b == r.b
	}
	return avText2Text(l) == avText2Text(r)
}

func avOrder(l, r aval) (int, bool) {
	if l.kind == avNum && r.kind == avNum {
		switch {
		case l.num < r.num:
			return -1, true
		case l.num > r.num:
			return 1, true
		}
		return 0, true
	}
	if l.kind == avText && r.kind == avText {
		return strings.Compare(l.text, r.text), true
	}
	return 0, false
}

func (e *blankEvaluator) evalIn(n *exprNode) aval {
	subject := e.eval(n.args[0])
	if subject.kind == avOpaque {
		return e.launder(subject)
	}
	derived := subject.derived
	if subject.kind == avNull {
		return nullVal(derived)
	}
	sawNull := false
	for _, a := range n.args[1:] {
		v := e.eval(a)
		if v.derived {
			derived = true
		}
		if v.kind == avOpaque {
			return e.launder(mergeDerived(subject, v))
		}
		if v.kind == avNull {
			sawNull = true
			continue
		}
		if avEqual(subject, v) {
			return boolVal(!n.neg, derived)
		}
	}
	if sawNull {
		return nullVal(derived)
	}
	return boolVal(n.neg, derived)
}

func (e *blankEvaluator) evalLike(n *exprNode) aval {
	l, r := e.eval(n.args[0]), e.eval(n.args[1])
	derived := l.derived || r.derived
	if l.kind == avOpaque || r.kind == avOpaque {
		return e.launder(mergeDerived(l, r))
	}
	if l.kind == avNull || r.kind == avNull {
		return nullVal(derived)
	}
	subject, pattern := avText2Text(l), avText2Text(r)
	if n.text == "ilike" {
		subject, pattern = strings.ToLower(subject), strings.ToLower(pattern)
	}
	m := likeMatch(subject, pattern)
	if n.neg {
		m = !m
	}
	return boolVal(m, derived)
}

// likeMatch is SQL LIKE with `%` and `_`. The gate only ever asks it about the
// three probe values, so it does not need to be fast.
func likeMatch(s, pattern string) bool {
	var match func(si, pi int) bool
	match = func(si, pi int) bool {
		for pi < len(pattern) {
			switch pattern[pi] {
			case '%':
				for k := si; k <= len(s); k++ {
					if match(k, pi+1) {
						return true
					}
				}
				return false
			case '_':
				if si >= len(s) {
					return false
				}
				si++
				pi++
			default:
				if si >= len(s) || s[si] != pattern[pi] {
					return false
				}
				si++
				pi++
			}
		}
		return si == len(s)
	}
	return match(0, 0)
}

func (e *blankEvaluator) evalBetween(n *exprNode) aval {
	subject, lo, hi := e.eval(n.args[0]), e.eval(n.args[1]), e.eval(n.args[2])
	derived := subject.derived || lo.derived || hi.derived
	if subject.kind == avOpaque || lo.kind == avOpaque || hi.kind == avOpaque {
		return e.launder(mergeDerived(subject, lo, hi))
	}
	if subject.kind == avNull || lo.kind == avNull || hi.kind == avNull {
		return nullVal(derived)
	}
	a, okA := avOrder(subject, lo)
	b, okB := avOrder(subject, hi)
	if !okA || !okB {
		return e.launder(opaqueVal(derived))
	}
	in := a >= 0 && b <= 0
	if n.neg {
		in = !in
	}
	return boolVal(in, derived)
}

func (e *blankEvaluator) evalCase(n *exprNode) aval {
	derived := false
	for i := 0; i+1 < len(n.args); i += 2 {
		cond, then := n.args[i], n.args[i+1]
		if cond == nil { // the ELSE arm
			v := e.eval(then)
			return withDerived(v, derived)
		}
		cv := e.eval(cond)
		if cv.derived {
			derived = true
		}
		switch cv.kind {
		case avBool:
			if cv.b {
				return withDerived(e.eval(then), derived)
			}
		case avNull:
			// a NULL condition is not taken
		default:
			return e.launder(opaqueVal(derived || cv.derived))
		}
	}
	return nullVal(derived)
}

// ---------------------------------------------------------------------------
// the three-point verdict
// ---------------------------------------------------------------------------

type blankVerdict int

const (
	// blankNotATest: the expression is not a predicate about this column's
	// blankness — rendering it, grouping by it, comparing it to a real value,
	// or asking the legitimate inverse "which rows have one".
	blankNotATest blankVerdict = iota
	// blankTestsForEmptiness: TRUE when the column is blank, FALSE when it is
	// filled. This is the false question, in whatever spelling.
	blankTestsForEmptiness
	// blankUnreadable: the column reaches a boolean predicate through
	// something this reader cannot evaluate, so it cannot say either way.
	blankUnreadable
)

// judgeBlanknessTest runs the three-point test on one expression.
//
// The rule, in one line: a predicate that is TRUE for a blank column and FALSE
// for a filled one IS the false question, whatever it is spelled like. The
// inverse — TRUE for a filled column — is a real question and passes.
func judgeBlanknessTest(n *exprNode, guarded map[string]bool) blankVerdict {
	run := func(probe aval) (aval, bool) {
		e := &blankEvaluator{guarded: guarded, probe: probe}
		v := e.eval(n)
		return v, e.laundered
	}
	vNull, lNull := run(nullVal(true))
	vEmpty, lEmpty := run(textVal("", true))
	vFilled, lFilled := run(textVal(blankProbePresent, true))

	blankIsTrue := (vNull.kind == avBool && vNull.b) || (vEmpty.kind == avBool && vEmpty.b)
	filledIsFalse := vFilled.kind == avBool && !vFilled.b
	if blankIsTrue && filledIsFalse {
		return blankTestsForEmptiness
	}
	if (lNull || lEmpty || lFilled) && allOpaque(vNull, vEmpty, vFilled) {
		return blankUnreadable
	}
	return blankNotATest
}

// allOpaque reports that every leg of the three-point test came back opaque,
// which — together with laundering — is what an unmodelled wrapper around this
// column looks like from the outside.
func allOpaque(vs ...aval) bool {
	for _, v := range vs {
		if v.kind != avOpaque {
			return false
		}
	}
	return len(vs) > 0
}
