package credential

import (
	"unicode"
	"unicode/utf8"
)

// ValidNewPassword applies only when setting a password, never to legacy login.
func ValidNewPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 8 || length > 128 {
		return false
	}
	var letter, digit, symbol bool
	for _, r := range password {
		letter = letter || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit = digit || r >= '0' && r <= '9'
		symbol = symbol || unicode.IsPunct(r) || unicode.IsSymbol(r)
	}
	return letter && digit && symbol
}
