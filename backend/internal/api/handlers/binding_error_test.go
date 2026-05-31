package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllHandlerShouldBindCallsCheckErrors(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read handlers dir failed: %v", err)
	}

	var unchecked []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s failed: %v", name, err)
		}
		for idx, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, ".ShouldBind") {
				continue
			}
			checked := strings.HasPrefix(trimmed, "if err :=") && strings.Contains(trimmed, "; err != nil")
			if !checked {
				unchecked = append(unchecked, filepath.ToSlash(name)+":"+itoa(idx+1)+" "+trimmed)
			}
		}
	}

	if len(unchecked) > 0 {
		t.Fatalf("unchecked ShouldBind calls:\n%s", strings.Join(unchecked, "\n"))
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := [20]byte{}
	pos := len(digits)
	for v > 0 {
		pos--
		digits[pos] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[pos:])
}
