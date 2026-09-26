//go:build !windows

package workspace

import "errors"

var errNoShortNames = errors.New("8.3 short names are a Windows concept")

// shortPathName has no meaning off Windows; the test compares the long spelling
// with itself and the FinalPath comparison still has to hold.
func shortPathName(string) (string, error) {
	return "", errNoShortNames
}
