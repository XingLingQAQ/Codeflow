//go:build !windows

package config

import "os"

func replaceSecretStoreFile(source, target string) error {
	return os.Rename(source, target)
}
