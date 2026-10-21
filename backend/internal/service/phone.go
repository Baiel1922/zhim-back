package service

import "strings"

// NormalizePhone converts a Kyrgyz phone number to +996XXXXXXXXX.
// Accepted inputs: +996 700 123 456, 996700123456, 0700123456, 700123456,
// with spaces, dashes and parentheses anywhere.
func NormalizePhone(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	digits := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits = append(digits, c)
		case c == ' ' || c == '-' || c == '(' || c == ')':
		default:
			return "", false
		}
	}
	d := string(digits)
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "996"):
	case len(d) == 10 && d[0] == '0':
		d = "996" + d[1:]
	case len(d) == 9:
		d = "996" + d
	default:
		return "", false
	}
	// Kyrgyz operator/area codes never start with 0 (that is the domestic trunk prefix).
	if d[3] == '0' {
		return "", false
	}
	return "+" + d, true
}
