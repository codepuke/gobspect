package main

import (
	"fmt"
	"strings"

	"github.com/codepuke/gobspect"
)

// ANSI escape sequences.
const (
	ansiReset    = "\x1b[0m"
	ansiDim      = "\x1b[2m"
	ansiGreen    = "\x1b[32m"
	ansiYellow   = "\x1b[33m"
	ansiCyan     = "\x1b[36m"
	ansiBoldCyan = "\x1b[1;36m"
	ansiMagenta  = "\x1b[35m"
)

// colorize applies ANSI color codes to a string produced by gobspect.Format.
// The input is processed line by line. Each line is classified and colored
// according to its role (type header, field, closing brace, scalar, etc.).
// Unrecognized lines are emitted verbatim so the text is never corrupted.
func colorize(s string) string {
	lines := strings.Split(s, "\n")
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(colorizeLine(line))
	}
	return sb.String()
}

// colorizeLine colors a single line from Format output.
func colorizeLine(line string) string {
	// Empty line.
	if line == "" {
		return line
	}

	// Measure leading whitespace (indentation).
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]

	// Pure closing brace (possibly indented): "}" or "  }"
	if trimmed == "}" {
		return indent + "}"
	}

	// Field line: "  FieldName: <value>"
	// Identified by containing ": " after stripping indent.
	if colonIdx := strings.Index(trimmed, ": "); colonIdx > 0 {
		fieldName := trimmed[:colonIdx]
		// Field names are plain identifiers (no spaces, no braces).
		// Guard against false positives like type headers "Foo{" containing ":".
		if isIdentifier(fieldName) {
			rest := trimmed[colonIdx+2:]
			return indent + ansiGreen + fieldName + ansiReset + ": " + colorizeValue(rest)
		}
	}

	// Type header: "TypeName{" or "TypeName{}" or "TypeName{ ... }" (inline collections)
	// Also handles "map[key]elem{...}" and "[]elem{...}" and "[N]elem{...}".
	if endsWithBrace(trimmed) || strings.Contains(trimmed, "{") {
		return colorizeHeader(indent, trimmed)
	}

	// Standalone scalar (e.g. when the root value is a scalar, printed alone).
	return indent + colorizeValue(trimmed)
}

// colorizeHeader colors a type-name-or-collection header line.
// Examples: "Foo{", "Foo{}", "[]string{1, 2}", "map[string]int{a: 1}".
func colorizeHeader(indent, trimmed string) string {
	braceIdx := strings.IndexByte(trimmed, '{')
	if braceIdx < 0 {
		return indent + trimmed
	}
	header := trimmed[:braceIdx]
	rest := trimmed[braceIdx:] // "{" or "{...}" or "{...}\n..."
	return indent + ansiBoldCyan + header + ansiReset + rest
}

// colorizeValue colors the value portion of a field line (the text after ": ").
func colorizeValue(s string) string {
	if s == "" {
		return s
	}

	// nil / booleans.
	switch s {
	case "nil", "true", "false":
		return ansiCyan + s + ansiReset
	}

	// Quoted string: starts with '"'.
	if s[0] == '"' {
		return ansiYellow + s + ansiReset
	}

	// Number: starts with a digit or '-' followed by digit.
	if isNumberStart(s) {
		return ansiMagenta + s + ansiReset
	}

	// Interface / opaque prefix: "(TypeName) value"
	if s[0] == '(' {
		closeIdx := strings.IndexByte(s, ')')
		if closeIdx > 0 && closeIdx+1 < len(s) {
			prefix := s[:closeIdx+1]
			rest := s[closeIdx+1:]
			// Trim the space between prefix and value.
			if len(rest) > 0 && rest[0] == ' ' {
				rest = rest[1:]
			}
			return ansiDim + prefix + ansiReset + " " + colorizeValue(rest)
		}
	}

	// Hex blob: all lowercase hex chars, possibly ending with "…".
	if isHexBlob(s) {
		return ansiDim + s + ansiReset
	}

	// Inline collection or nested struct header on one line: contains '{'.
	if strings.ContainsRune(s, '{') {
		return colorizeHeader("", s)
	}

	// Fallback: emit as-is.
	return s
}

// isIdentifier reports whether s looks like a Go identifier (no spaces or
// structural characters). This guards against false positives in colorizeLine.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c == ' ' || c == '{' || c == '}' || c == '[' || c == ']' || c == '(' || c == ')' {
			return false
		}
	}
	return true
}

// isNumberStart reports whether the string looks like the start of a number.
func isNumberStart(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return true
	}
	if s[0] == '-' && len(s) > 1 && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	// Complex: starts with '('
	if s[0] == '(' && len(s) > 1 {
		// Could be a complex number like "(1.5+2i)" — check for digits after '('.
		if s[1] >= '0' && s[1] <= '9' {
			return true
		}
		if s[1] == '-' && len(s) > 2 && s[2] >= '0' && s[2] <= '9' {
			return true
		}
	}
	return false
}

// endsWithBrace reports whether s ends with '}' (possibly an inline closing).
func endsWithBrace(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '}'
}

// isHexBlob reports whether s is a hex-encoded byte sequence as emitted by
// fmtHex: lowercase hex chars (even count) optionally suffixed with "…".
func isHexBlob(s string) bool {
	if s == "" {
		return false
	}
	// Strip optional trailing ellipsis.
	check := strings.TrimSuffix(s, "…")
	if len(check) == 0 || len(check)%2 != 0 {
		return false
	}
	for _, c := range check {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// colorizeSchema applies syntax highlighting to a *gobspect.Schema directly.
func colorizeSchema(s *gobspect.Schema) string {
	var sb strings.Builder
	indent := s.Indent
	if indent == "" {
		indent = "  "
	}

	for i, t := range s.Types {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		switch t.Kind {
		case gobspect.KindStruct:
			if len(t.Fields) == 0 {
				sb.WriteString(ansiMagenta + "type" + ansiReset + " " + ansiBoldCyan + t.Name + ansiReset + " struct{}")
				continue
			}

			// Determine max len to align all field types.
			maxLen := 0
			for _, f := range t.Fields {
				if len(f.Name) > maxLen {
					maxLen = len(f.Name)
				}
			}

			sb.WriteString(ansiMagenta + "type" + ansiReset + " " + ansiBoldCyan + t.Name + ansiReset + " struct {\n")
			for _, f := range t.Fields {
				sb.WriteString(indent)
				sb.WriteString(ansiGreen + f.Name + ansiReset)
				// Pad so all type expressions start at the same column (maxLen + 2 spaces).
				sb.WriteString(strings.Repeat(" ", maxLen-len(f.Name)+2))
				sb.WriteString(f.Type)
				if f.Annotation != "" {
					sb.WriteString("  " + ansiDim + "// " + f.Annotation + ansiReset)
				}
				sb.WriteString("\n")
			}
			sb.WriteString("}")

		case gobspect.KindMap, gobspect.KindSlice, gobspect.KindArray:
			sb.WriteString(fmt.Sprintf("%stype%s %s %s", ansiMagenta, ansiReset, ansiBoldCyan+t.Name+ansiReset, t.TargetType))
		case gobspect.KindGobEncoder, gobspect.KindBinaryMarshaler, gobspect.KindTextMarshaler:
			sb.WriteString(fmt.Sprintf("%stype%s %s %s// %s%s", ansiMagenta, ansiReset, ansiBoldCyan+t.Name+ansiReset, ansiDim, t.Annotation, ansiReset))
		default:
			sb.WriteString(ansiDim + "// type " + t.Name + ansiReset)
		}
	}
	return sb.String()
}
