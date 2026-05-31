package internal_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDatabaseResourcesUseDeferredCloseOrRollback(t *testing.T) {
	resourceCall := regexp.MustCompile(`\b(?:[A-Za-z_]*[Rr]ows|stmt)\.Close\(\)|\btx\.Rollback\(\)`)
	var violations []string

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for idx, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if resourceCall.MatchString(trimmed) && !strings.HasPrefix(trimmed, "defer ") {
				violations = append(violations, fmt.Sprintf("%s:%d %s", filepath.ToSlash(path), idx+1, trimmed))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan backend/internal failed: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("database resources must use defer for Close/Rollback:\n%s", strings.Join(violations, "\n"))
	}
}
