// Package ident is the other half of the duplicate-name candidate pair of
// this fixture. See slug.Normalize for the same-name function with a
// different implementation.
package ident

import "strings"

// Normalize uppercases the input and joins words with underscores.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	return strings.ReplaceAll(s, " ", "_")
}
