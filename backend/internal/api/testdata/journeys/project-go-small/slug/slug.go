// Package slug is one half of the duplicate-name candidate pair of this
// fixture. See ident.Normalize for the same-name function with a different
// implementation.
package slug

import "strings"

// Normalize lowercases the input and joins words with dashes.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, " ", "-")
}
