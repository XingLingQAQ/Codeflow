package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testAbsPath 用平台无关的方式构造一个绝对、已清理的测试路径。
func testAbsPath(elem ...string) string {
	return filepath.Join(os.TempDir(), filepath.Join(elem...))
}

// testUncleaned 构造一个绝对但未清理的路径（含 ".." 段）。
func testUncleaned() string {
	sep := string(filepath.Separator)
	return testAbsPath("codeflow-process-test") + sep + "nested" + sep + ".." + sep + "helper.exe"
}

func TestOwnershipValidate(t *testing.T) {
	cases := []struct {
		name string
		own  Ownership
	}{
		{"valid", Ownership{RunID: "run-1", AttemptID: "attempt-1", OwnerInstance: "instance-a"}},
		{"missing run id", Ownership{AttemptID: "attempt-1", OwnerInstance: "instance-a"}},
		{"blank run id", Ownership{RunID: "   ", AttemptID: "attempt-1", OwnerInstance: "instance-a"}},
		{"missing attempt id", Ownership{RunID: "run-1", OwnerInstance: "instance-a"}},
		{"blank attempt id", Ownership{RunID: "run-1", AttemptID: "\t", OwnerInstance: "instance-a"}},
		{"missing owner instance", Ownership{RunID: "run-1", AttemptID: "attempt-1"}},
		{"blank owner instance", Ownership{RunID: "run-1", AttemptID: "attempt-1", OwnerInstance: "\n"}},
		{"all empty", Ownership{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.own.Validate()
			wantErr := tc.name != "valid"
			if wantErr && err == nil {
				t.Fatalf("Ownership%+v.Validate() = nil, want error", tc.own)
			}
			if !wantErr && err != nil {
				t.Fatalf("Ownership%+v.Validate() = %v, want nil", tc.own, err)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "process:") {
				t.Fatalf("error %q is not prefixed with %q", err, "process:")
			}
		})
	}
}

func validSpec() Spec {
	return Spec{
		Path:          testAbsPath("codeflow-process-test", "helper.exe"),
		Args:          []string{"-mode", "tree", "-depth", "2"},
		Dir:           testAbsPath("codeflow-process-test"),
		Env:           []string{"PATH=/usr/bin", "SystemRoot=C:\\Windows"},
		Owner:         testOwnership(),
		GracePeriod:   time.Second,
		MaxSpoolBytes: 1 << 20,
	}
}

func TestSpecValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Spec)
		wantErr bool
	}{
		{"valid", func(*Spec) {}, false},
		{"nil env and zero durations use defaults", func(s *Spec) {
			s.Env = nil
			s.GracePeriod = 0
			s.MaxSpoolBytes = 0
		}, false},
		{"empty path", func(s *Spec) { s.Path = "" }, true},
		{"blank path", func(s *Spec) { s.Path = "   " }, true},
		{"nul in path", func(s *Spec) { s.Path = testAbsPath("a\x00b") }, true},
		{"relative path", func(s *Spec) { s.Path = "helper.exe" }, true},
		{"uncleaned path", func(s *Spec) { s.Path = testUncleaned() }, true},
		{"nul in arg", func(s *Spec) { s.Args = []string{"-x", "a\x00b"} }, true},
		{"empty dir", func(s *Spec) { s.Dir = "" }, true},
		{"relative dir", func(s *Spec) { s.Dir = "work" }, true},
		{"uncleaned dir", func(s *Spec) { s.Dir = testUncleaned() }, true},
		{"env entry without equals", func(s *Spec) { s.Env = []string{"PATH"} }, true},
		{"env entry with empty key", func(s *Spec) { s.Env = []string{"=value"} }, true},
		{"env duplicate key", func(s *Spec) { s.Env = []string{"A=1", "A=2"} }, true},
		{"env duplicate key differing only in case", func(s *Spec) {
			s.Env = []string{"Path=a", "PATH=b"}
		}, runtime.GOOS == "windows"},
		{"env entry with nul", func(s *Spec) { s.Env = []string{"A=b\x00c"} }, true},
		{"missing owner", func(s *Spec) { s.Owner = Ownership{} }, true},
		{"owner missing instance", func(s *Spec) {
			s.Owner = Ownership{RunID: "run-1", AttemptID: "attempt-1"}
		}, true},
		{"negative grace period", func(s *Spec) { s.GracePeriod = -time.Second }, true},
		{"negative max spool bytes", func(s *Spec) { s.MaxSpoolBytes = -1 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			err := spec.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Spec.Validate() = nil for %+v, want error", spec)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Spec.Validate() = %v for %+v, want nil", err, spec)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "process:") {
				t.Fatalf("error %q is not prefixed with %q", err, "process:")
			}
		})
	}
}

// TestSpecValidateDoesNotTouchFilesystem 固定“Validate 只做结构校验”这一约定：
// 路径不需要存在，存在性与可执行性检查是 Start 的职责。
func TestSpecValidateDoesNotTouchFilesystem(t *testing.T) {
	spec := validSpec()
	spec.Path = testAbsPath("codeflow-process-missing-dir-108a", "does-not-exist.exe")
	spec.Dir = testAbsPath("codeflow-process-missing-dir-108a")
	if _, err := os.Stat(spec.Path); err == nil {
		t.Fatalf("test prerequisite: %s unexpectedly exists", spec.Path)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Spec.Validate() = %v, want nil (must not require the path to exist)", err)
	}
}

func TestCancelModeAndExitReasonValid(t *testing.T) {
	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		if !mode.Valid() {
			t.Fatalf("CancelMode(%q).Valid() = false, want true", mode)
		}
	}
	for _, mode := range []CancelMode{"", "kill", "SOFT"} {
		if mode.Valid() {
			t.Fatalf("CancelMode(%q).Valid() = true, want false", mode)
		}
	}
	for _, reason := range []ExitReason{ExitExited, ExitCancelled, ExitKilled, ExitTimeout, ExitLost} {
		if !reason.Valid() {
			t.Fatalf("ExitReason(%q).Valid() = false, want true", reason)
		}
	}
	for _, reason := range []ExitReason{"", "oom", "EXITED"} {
		if reason.Valid() {
			t.Fatalf("ExitReason(%q).Valid() = true, want false", reason)
		}
	}
}

// TestProcessDefaults 固定零值语义与 §27.6 的 5s grace 约定。
func TestProcessDefaults(t *testing.T) {
	if DefaultGracePeriod != 5*time.Second {
		t.Fatalf("DefaultGracePeriod = %s, want 5s (§27.6)", DefaultGracePeriod)
	}
	if DefaultMaxSpoolBytes <= 0 {
		t.Fatalf("DefaultMaxSpoolBytes = %d, want positive", DefaultMaxSpoolBytes)
	}
}
