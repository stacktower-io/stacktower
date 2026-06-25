package sbom

import (
	"regexp"
	"strings"
)

// spdxTokenPattern matches a single SPDX license/exception id token, e.g.
// "MIT", "Apache-2.0", "GPL-3.0-or-later", "LicenseRef-custom".
var spdxTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)

// isSPDXID reports whether s has the shape of a single SPDX license id
// (no operators, no whitespace, no parentheses).
func isSPDXID(s string) bool {
	return spdxTokenPattern.MatchString(s)
}

// isSPDXExpression reports whether s parses as an SPDX license expression:
//
//	expr := term (("AND" | "OR") term)*
//	term := "(" expr ")" | id ["WITH" id]
//
// Individual ids are validated by shape only (we don't ship the SPDX license
// list); the goal is distinguishing structured expressions like
// "MIT OR Apache-2.0" from free-text license strings.
func isSPDXExpression(s string) bool {
	tokens := tokenizeSPDX(s)
	if len(tokens) == 0 {
		return false
	}

	pos := 0
	peek := func() string {
		if pos < len(tokens) {
			return tokens[pos]
		}
		return ""
	}

	var parseExpr func() bool
	parseTerm := func() bool {
		switch tok := peek(); {
		case tok == "(":
			pos++
			if !parseExpr() || peek() != ")" {
				return false
			}
			pos++
			return true
		case isSPDXID(tok) && tok != "AND" && tok != "OR" && tok != "WITH":
			pos++
			if peek() == "WITH" {
				pos++
				exception := peek()
				if !isSPDXID(exception) {
					return false
				}
				pos++
			}
			return true
		default:
			return false
		}
	}
	parseExpr = func() bool {
		if !parseTerm() {
			return false
		}
		for peek() == "AND" || peek() == "OR" {
			pos++
			if !parseTerm() {
				return false
			}
		}
		return true
	}

	return parseExpr() && pos == len(tokens)
}

// tokenizeSPDX splits an SPDX expression into tokens, treating parentheses
// as standalone tokens.
func tokenizeSPDX(s string) []string {
	s = strings.ReplaceAll(s, "(", " ( ")
	s = strings.ReplaceAll(s, ")", " ) ")
	return strings.Fields(s)
}
