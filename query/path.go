package query

import (
	"fmt"
	"strconv"
	"strings"
)

// segKind classifies one segment of a compiled path.
type segKind int

const (
	segField    segKind = iota // named struct field or map[string] key
	segIndex                   // integer index (may be negative)
	segWildcard                // * — all elements
	segFilter                  // [Field!], [Field=pattern], or [Field~pattern]
	segDescend                 // ..name recursive descent (or wildcard descent when name is empty)
	segProject                 // A,B,C — multi-field projection
)

// filterOp classifies the operator in a segFilter segment.
type filterOp int

const (
	filterOpExist       filterOp = iota // [Field!]
	filterOpGlob                        // [Field=pattern]
	filterOpContains                    // [Field~pattern]
	filterOpNotExist                    // [Field!!]
	filterOpNegGlob                     // [Field!=pattern]
	filterOpNegContains                 // [Field!~pattern]
	filterOpNumEq                       // [Field==value]
	filterOpNumLT                       // [Field<value]
	filterOpNumGT                       // [Field>value]
	filterOpNumLTE                      // [Field<=value]
	filterOpNumGTE                      // [Field>=value]
)

// segment is one parsed element of a path.
type segment struct {
	kind          segKind
	name          string    // segField: field/key name; segFilter: filter field name; segDescend: descent name
	index         int       // segIndex: index value (negative = from end)
	filterOp      filterOp  // segFilter: which operator ([Field!], [Field=pattern], [Field~pattern])
	filterPattern string    // segFilter [Field=pattern] or [Field~pattern] form
	filterNumVal  float64   // parsed numeric value for numeric comparison ops
	filterNumOK   bool      // true if filterNumVal was successfully parsed
	filterIntVal  int64     // parsed int64 value for high-precision comparison
	filterIntOK   bool      // true if filterIntVal was successfully parsed
	filterUintVal uint64    // parsed uint64 value for high-precision comparison
	filterUintOK  bool      // true if filterUintVal was successfully parsed
	filterBoolVal bool      // parsed bool value for == comparisons on bool fields
	filterBoolOK  bool      // true if filterBoolVal was successfully parsed
	orAlts        []segment // non-nil = OR group; holds 2+ segFilter alternatives
	projectFields []string  // segProject: field names to project
}

// Path is a compiled, reusable path expression. Obtain one with [Parse].
// The zero value is a valid empty path (identity: resolves to the root).
type Path struct {
	segs []segment
}

// String returns the canonical string representation of the path.
// The zero-value Path (no segments) returns "".
// The output is the canonical form that round-trips through [Parse].
func (p Path) String() string {
	if len(p.segs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range p.segs {
		switch s.kind {
		case segField:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.name)
		case segIndex:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(strconv.Itoa(s.index))
		case segWildcard:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteByte('*')
		case segFilter:
			// No dot prefix; filter attaches directly to the preceding segment.
			if len(s.orAlts) > 0 {
				for j, alt := range s.orAlts {
					if j > 0 {
						b.WriteByte('|')
					}
					emitFilterSeg(&b, alt)
				}
			} else {
				emitFilterSeg(&b, s)
			}
		case segDescend:
			// No dot prefix; '..' already carries the separator.
			b.WriteString("..")
			b.WriteString(s.name) // empty string for wildcard descent
		case segProject:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(strings.Join(s.projectFields, ","))
		}
	}
	return b.String()
}

// IsEmpty reports whether the path has no segments (identity path).
func (p Path) IsEmpty() bool { return len(p.segs) == 0 }

// Parse compiles a dot-separated path expression into a [Path].
// Returns a [*ParseError] if the expression is syntactically invalid.
//
// An empty expression is valid and produces an identity path.
func Parse(expr string) (Path, error) {
	if expr == "" {
		return Path{}, nil
	}
	segs, err := parseSegments(expr)
	if err != nil {
		if pe, ok := err.(*ParseError); ok {
			pe.Expr = expr
		}
		return Path{}, err
	}
	return Path{segs: segs}, nil
}

// parseSegments tokenises expr into a slice of segments.
func parseSegments(expr string) ([]segment, error) {
	var segs []segment
	i := 0

	for i < len(expr) {
		// Handle separator between segments — only required when we already
		// have at least one segment, unless the next char is '[' (inline filter).
		if len(segs) > 0 {
			switch expr[i] {
			case '[':
				// Inline filter: no dot needed.
			case '.':
				// Check for '..' (named recursive descent).
				if i+1 < len(expr) && expr[i+1] == '.' {
					seg, n, err := parseDescentAt(expr, i)
					if err != nil {
						return nil, err
					}
					segs = append(segs, seg)
					i += n
					continue
				}
				i++ // consume the single '.'
				if i >= len(expr) {
					return nil, parseErr(i-1, "trailing '.'")
				}
			default:
				return nil, parseErr(i, "expected '.' between segments")
			}
		} else if expr[i] == '.' {
			// Leading dot.
			if i+1 < len(expr) && expr[i+1] == '.' {
				// Leading '..' — parse a descent segment.
				seg, n, err := parseDescentAt(expr, i)
				if err != nil {
					return nil, err
				}
				segs = append(segs, seg)
				i += n
				continue
			}
			return nil, parseErr(0, "path may not start with '.'")
		}

		if i >= len(expr) {
			break
		}

		switch expr[i] {
		case '[':
			seg, n, err := parseFilter(expr, i)
			if err != nil {
				return nil, err
			}
			segs = append(segs, seg)
			i += n

			// Collect any OR alternatives that immediately follow.
			for i < len(expr) && expr[i] == '|' {
				i++ // consume '|'
				if i >= len(expr) || expr[i] != '[' {
					return nil, parseErr(i-1, "'|' must be followed by a filter '[...]'")
				}
				nextSeg, n2, err := parseFilter(expr, i)
				if err != nil {
					return nil, err
				}
				i += n2
				last := &segs[len(segs)-1]
				if len(last.orAlts) == 0 {
					// Convert last single filter into an OR group.
					orig := *last
					last.orAlts = []segment{orig, nextSeg}
					// Clear parent's filter fields (they're now in orAlts).
					last.name = ""
					last.filterOp = 0
					last.filterPattern = ""
					last.filterNumVal = 0
					last.filterNumOK = false
					last.filterIntVal = 0
					last.filterIntOK = false
					last.filterUintVal = 0
					last.filterUintOK = false
					last.filterBoolVal = false
					last.filterBoolOK = false
				} else {
					last.orAlts = append(last.orAlts, nextSeg)
				}
			}

		case '*':
			segs = append(segs, segment{kind: segWildcard})
			i++

		default:
			// Read a name or integer token up to the next separator.
			j := i
			for j < len(expr) && expr[j] != '.' && expr[j] != '[' && expr[j] != '|' && expr[j] != '*' {
				j++
			}
			token := expr[i:j]
			i = j

			if token == "" {
				return nil, parseErr(i, "empty segment")
			}

			if strings.ContainsAny(token, "|") {
				return nil, parseErr(i-len(token), "unexpected '|' (OR is only allowed between filters)")
			}

			if seg, ok := parseToken(token); ok {
				segs = append(segs, seg)
			} else {
				return nil, parseErrf(i-len(token), "invalid segment %q", token)
			}

			// A filter bracket may follow a name token directly (no dot).
			// Don't consume it here; the next iteration handles it.
		}
	}

	if len(segs) == 0 {
		return nil, parseErr(-1, "path produced no segments")
	}
	return segs, nil
}

// parseDescentAt parses a '..' descent segment starting at position pos in expr
// (expr[pos] and expr[pos+1] must both be '.').
// Returns the segment and the total number of bytes consumed (including the two dots and the name token).
func parseDescentAt(expr string, pos int) (segment, int, error) {
	// pos and pos+1 are the two dots; name starts at pos+2.
	nameStart := pos + 2
	if nameStart >= len(expr) {
		return segment{}, 0, parseErr(pos+2, "trailing '..'")
	}
	switch expr[nameStart] {
	case '[':
		// Wildcard descent: '..' immediately followed by '[...filter...]'.
		// Produce a segDescend with empty name; the '[' filter is parsed as the
		// next segment by the normal loop.  Consume only the two dots.
		return segment{kind: segDescend, name: ""}, 2, nil
	case '*':
		return segment{}, 0, parseErr(nameStart, "recursive descent followed by '*' is reserved")
	}
	// Read name token up to the next separator.
	j := nameStart
	for j < len(expr) && expr[j] != '.' && expr[j] != '[' && expr[j] != '|' && expr[j] != '*' {
		j++
	}
	token := expr[nameStart:j]
	if token == "" {
		return segment{}, 0, parseErr(pos+2, "trailing '..'")
	}
	return segment{kind: segDescend, name: token}, j - pos, nil
}

// parseToken converts a raw token string to a segment.
// Returns (segment{}, false) for tokens that cannot be parsed.
func parseToken(token string) (segment, bool) {
	// Check for comma-separated projection (e.g. "SKU,Price" or "SKU,Address/Zip").
	if strings.Contains(token, ",") {
		parts := strings.Split(token, ",")
		fields := make([]string, 0, len(parts))
		leafSeen := make(map[string]bool, len(parts))
		for _, p := range parts {
			f := strings.TrimSpace(p)
			if f == "" {
				return segment{}, false
			}
			// Validate each /‑separated component individually.
			components := strings.Split(f, "/")
			for _, c := range components {
				if c == "" {
					return segment{}, false // leading, trailing, or double slash
				}
				if strings.ContainsAny(c, ".[]!=~<>|* /") {
					return segment{}, false
				}
			}
			leaf := components[len(components)-1]
			if leafSeen[leaf] {
				return segment{}, false // duplicate column name
			}
			leafSeen[leaf] = true
			fields = append(fields, f)
		}
		return segment{kind: segProject, projectFields: fields}, true
	}

	if isIntegerToken(token) {
		n, err := strconv.Atoi(token)
		if err != nil {
			// strconv.Atoi can fail for overflow; treat as a field name.
			return segment{kind: segField, name: token}, true
		}
		return segment{kind: segIndex, index: n}, true
	}
	if strings.ContainsAny(token, ".[]!=~<>|* ") {
		return segment{}, false
	}
	// Anything else that is non-empty is a field/key name.
	return segment{kind: segField, name: token}, true
}

// isIntegerToken reports whether s represents an integer (possibly negative).
func isIntegerToken(s string) bool {
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' {
		if len(s) == 1 {
			return false // bare "-" is not an integer
		}
		start = 1
	}
	for _, c := range s[start:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseFilter parses a [Field!] or [Field=pattern] expression starting at
// position start (which must point at '[').
// Returns the segment, the number of bytes consumed, and any error.
func parseFilter(expr string, start int) (segment, int, error) {
	// Find the first ']' for suffix-only operators and unquoted pattern operators.
	rest := expr[start+1:] // everything after '['
	basicInner, _, found := strings.Cut(rest, "]")
	if !found {
		return segment{}, 0, parseErr(start, "unclosed '['")
	}

	if basicInner == "" {
		return segment{}, 0, parseErr(start, "empty filter '[]'")
	}

	// [Field!!] — not-exist filter (no pattern, safe to use basicInner).
	if strings.HasSuffix(basicInner, "!!") {
		field := basicInner[:len(basicInner)-2]
		if field == "" {
			return segment{}, 0, parseErr(start+1, "filter field name is empty")
		}
		if strings.ContainsAny(field, ".[]!=~<> ") {
			return segment{}, 0, parseErrf(start+1, "invalid filter field name %q", field)
		}
		consumed := 1 + len(basicInner) + 1 // '[' + inner + ']'
		return segment{kind: segFilter, name: field, filterOp: filterOpNotExist}, consumed, nil
	}

	// [Field!] — existence filter (no pattern, safe to use basicInner).
	if strings.HasSuffix(basicInner, "!") {
		field := basicInner[:len(basicInner)-1]
		if field == "" {
			return segment{}, 0, parseErr(start+1, "filter field name is empty")
		}
		if strings.ContainsAny(field, ".[]!=~<> ") {
			return segment{}, 0, parseErrf(start+1, "invalid filter field name %q", field)
		}
		consumed := 1 + len(basicInner) + 1 // '[' + inner + ']'
		return segment{kind: segFilter, name: field, filterOp: filterOpExist}, consumed, nil
	}

	// For pattern operators, check for quoted patterns by scanning from the operator.
	type opCandidate struct {
		sep     string
		op      filterOp
		numeric bool // true if this operator uses numeric comparison
	}
	candidates := []opCandidate{
		{"!=", filterOpNegGlob, false},
		{"!~", filterOpNegContains, false},
		{"==", filterOpNumEq, true},
		{">=", filterOpNumGTE, true},
		{"<=", filterOpNumLTE, true},
		{">", filterOpNumGT, true},
		{"<", filterOpNumLT, true},
		{"=", filterOpGlob, false},
		{"~", filterOpContains, false},
	}

	var lastErr error
	for _, c := range candidates {
		idx := strings.Index(basicInner, c.sep)
		if idx < 0 {
			continue
		}
		field := basicInner[:idx]
		if field == "" {
			if lastErr == nil {
				lastErr = parseErr(start+1, "filter field name is empty")
			}
			continue
		}
		if strings.ContainsAny(field, ".[]!=~<> ") {
			if lastErr == nil {
				// Suggest quoting if the user might be confusing the parser
				lastErr = parseErrf(start+1, "invalid filter field name %q (quote pattern if it contains operators)", field)
			}
			continue
		}

		// Position of the character right after the operator separator in expr.
		patternStart := start + 1 + idx + len(c.sep)

		var pattern string
		var consumed int

		if patternStart < len(expr) && expr[patternStart] == '"' {
			// Quoted pattern: scan forward for closing '"'.
			quoted, afterQuote, ok := scanQuotedPattern(expr, patternStart)
			if !ok {
				return segment{}, 0, parseErr(patternStart, "unclosed quoted pattern")
			}
			// After the closing '"', must have ']'.
			if afterQuote >= len(expr) || expr[afterQuote] != ']' {
				return segment{}, 0, parseErr(patternStart, "unclosed quoted pattern")
			}
			decoded, err := unquotePattern(quoted)
			if err != nil {
				return segment{}, 0, parseErr(patternStart, "unclosed quoted pattern")
			}
			pattern = decoded
			consumed = afterQuote + 1 - start // '[' ... ']'
		} else {
			// Unquoted pattern: use the basicInner already found.
			pattern = basicInner[idx+len(c.sep):]
			consumed = 1 + len(basicInner) + 1 // '[' + inner + ']'
		}

		seg := segment{kind: segFilter, name: field, filterOp: c.op, filterPattern: pattern}
		if c.numeric {
			val, err := strconv.ParseFloat(pattern, 64)
			if err == nil {
				seg.filterNumVal = val
				seg.filterNumOK = true

				// Also try parsing as int64 and uint64 for high-precision integer comparisons.
				// We only do this if the pattern is a pure integer literal (no '.' or 'e').
				if !strings.ContainsAny(pattern, ".eE") {
					if iv, err := strconv.ParseInt(pattern, 10, 64); err == nil {
						seg.filterIntVal = iv
						seg.filterIntOK = true
					}
					if uv, err := strconv.ParseUint(pattern, 10, 64); err == nil {
						seg.filterUintVal = uv
						seg.filterUintOK = true
					}
				}
			} else {
				// Accept bool literals (case-insensitive); reject everything else at parse time.
				switch strings.ToLower(pattern) {
				case "true":
					seg.filterBoolOK = true
					seg.filterBoolVal = true
				case "false":
					seg.filterBoolOK = true
					seg.filterBoolVal = false
				default:
					return segment{}, 0, parseErrf(patternStart, "invalid filter value %q: expected a number or bool literal (true/false)", pattern)
				}
			}
		}
		return seg, consumed, nil
	}

	if lastErr != nil {
		return segment{}, 0, lastErr
	}
	return segment{}, 0, parseErrf(start, "invalid filter %q: expected '!' suffix or '=' separator", basicInner)
}

// scanQuotedPattern scans forward from pos (which points at the opening '"')
// to find the closing '"', honouring '\"' as an escape.
// Returns the full quoted string (including the surrounding quotes) and the
// position just after the closing '"'. The bool is false if no closing quote was found.
func scanQuotedPattern(expr string, pos int) (string, int, bool) {
	i := pos + 1
	for i < len(expr) {
		if expr[i] == '\\' && i+1 < len(expr) {
			i += 2 // skip escaped character
			continue
		}
		if expr[i] == '"' {
			// Found closing quote; return the quoted string including delimiters.
			return expr[pos : i+1], i + 1, true
		}
		i++
	}
	return "", 0, false
}

// unquotePattern decodes a quoted pattern string. s must start with '"' and end with '"'.
// Returns an error if the closing '"' is missing or s doesn't start with '"'.
func unquotePattern(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("unclosed quoted pattern")
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	i := 0
	for i < len(inner) {
		if inner[i] == '\\' && i+1 < len(inner) {
			switch inner[i+1] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte('\\')
				b.WriteByte(inner[i+1])
			}
			i += 2
		} else {
			b.WriteByte(inner[i])
			i++
		}
	}
	return b.String(), nil
}

// emitPattern writes p to b, quoting it if necessary.
func emitPattern(b *strings.Builder, p string) {
	if needsQuoting(p) {
		b.WriteString(quotePattern(p))
	} else {
		b.WriteString(p)
	}
}

// emitFilterSeg writes a single filter segment as [Name op pattern] to b.
func emitFilterSeg(b *strings.Builder, s segment) {
	b.WriteByte('[')
	b.WriteString(s.name)
	switch s.filterOp {
	case filterOpExist:
		b.WriteByte('!')
	case filterOpGlob:
		b.WriteByte('=')
		emitPattern(b, s.filterPattern)
	case filterOpContains:
		b.WriteByte('~')
		emitPattern(b, s.filterPattern)
	case filterOpNotExist:
		b.WriteString("!!")
	case filterOpNegGlob:
		b.WriteString("!=")
		emitPattern(b, s.filterPattern)
	case filterOpNegContains:
		b.WriteString("!~")
		emitPattern(b, s.filterPattern)
	case filterOpNumEq:
		b.WriteString("==")
		emitPattern(b, s.filterPattern)
	case filterOpNumLT:
		b.WriteByte('<')
		emitPattern(b, s.filterPattern)
	case filterOpNumGT:
		b.WriteByte('>')
		emitPattern(b, s.filterPattern)
	case filterOpNumLTE:
		b.WriteString("<=")
		emitPattern(b, s.filterPattern)
	case filterOpNumGTE:
		b.WriteString(">=")
		emitPattern(b, s.filterPattern)
	}
	b.WriteByte(']')
}

// needsQuoting reports whether a filter pattern must be emitted in quoted form.
func needsQuoting(p string) bool {
	if len(p) > 0 && p[0] == '"' {
		return true
	}
	// A trailing '!' would be misinterpreted as the existence-filter suffix
	// ([Field!]) when the pattern is re-parsed, so quote it.
	if len(p) > 0 && p[len(p)-1] == '!' {
		return true
	}
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case ']', '"', '\\', '!', '~', '=', '<', '>':
			return true
		}
		// Control characters (newline, tab, CR, etc.) must be escaped.
		if p[i] < 0x20 {
			return true
		}
	}
	return false
}

// quotePattern emits the pattern in quoted form with special characters escaped.
func quotePattern(p string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(p[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
