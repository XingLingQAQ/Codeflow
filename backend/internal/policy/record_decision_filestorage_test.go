package policy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

// setupDecisionFileAudit 与 setupDecisionAudit 同型，但安装真实文件审计存储
// （t.TempDir），用于 T0.10.c 要求的真实 storage 链 hash 复核。
func setupDecisionFileAudit(t *testing.T) (*audit.AuditService, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &audit.FileStorageConfig{
		LogDir:          dir,
		FilePrefix:      "decision",
		MaxFileSize:     1 << 20,
		MaxFiles:        2,
		VerifyOnStartup: true,
		FlushInterval:   60000,
	}
	store, err := audit.CreateFileAuditStorage(cfg)
	if err != nil {
		t.Fatalf("CreateFileAuditStorage: %v", err)
	}
	svc := audit.NewAuditService(store)
	audit.SetAuditService(svc)
	SetEvaluator(NewFailClosedEvaluator())
	t.Cleanup(func() {
		SetEvaluator(nil)
		audit.SetAuditService(nil)
	})
	return svc, dir
}

func decisionLogFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "decision*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob decision log files: files=%v err=%v", files, err)
	}
	return files
}

// TestRecordDecisionRedactionHashChainOnFileStorage 验证脱敏先于哈希落盘、且
// 真实文件存储上的链可复核：含脱敏条目的链 VerifyHashChain 有效；关闭重开
// （启动校验）通过；把落盘内容里的脱敏占位改回原始敏感值后，条目哈希变化、
// 启动完整性校验拒绝打开——篡改可检出。
func TestRecordDecisionRedactionHashChainOnFileStorage(t *testing.T) {
	svc, dir := setupDecisionFileAudit(t)
	ctx := context.Background()
	const canary = "canary-file-7"

	decision := Evaluate(ctx, Request{
		Operation: OperationPluginInvoke, Resource: "res-file", ProjectID: "proj token=" + canary,
		RequestID: "req-file-1",
	})
	if decision.AuditID == "" {
		t.Fatal("expected audit id on denied decision")
	}
	entry := mustGetEntry(t, svc, decision.AuditID)
	if entry.Details["project_id"] != redactedPlaceholder {
		t.Fatalf("expected redacted project_id, got %#v", entry.Details["project_id"])
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if strings.Contains(string(raw), canary) {
		t.Fatalf("canary leaked into persisted entry: %s", raw)
	}
	// 篡改敏感值必然改变条目哈希（脱敏内容才是哈希输入）。
	tampered := *entry
	tampered.Details = map[string]interface{}{"project_id": "proj token=" + canary}
	if audit.CalculateEntryHash(&tampered) == entry.Hash {
		t.Fatal("hash must change when redacted content is replaced with raw secret")
	}

	// 含脱敏条目的链在真实文件存储上复核有效（VerifyChain 先落盘再逐条验链）。
	result, err := svc.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.Valid || result.CheckedEntries != 1 {
		t.Fatalf("hash chain over redacted entry must verify on file storage: %+v", result)
	}

	// 重启证据：关闭后重开，启动完整性校验通过。
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := audit.CreateFileAuditStorage(&audit.FileStorageConfig{
		LogDir: dir, FilePrefix: "decision", MaxFileSize: 1 << 20, MaxFiles: 2,
		VerifyOnStartup: true, FlushInterval: 60000,
	})
	if err != nil {
		t.Fatalf("reopen with startup verification must succeed: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened: %v", err)
	}

	// 篡改落盘文件：把脱敏占位改回原始敏感值。
	files := decisionLogFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected exactly 1 log file, got %v", files)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), redactedPlaceholder) {
		t.Fatalf("persisted file must contain the redacted placeholder: %s", content)
	}
	forged := strings.Replace(string(content), redactedPlaceholder, "proj token="+canary, 1)
	if forged == string(content) {
		t.Fatal("tamper replacement did not change file content")
	}
	if err := os.WriteFile(files[0], []byte(forged), 0644); err != nil {
		t.Fatalf("write tampered file: %v", err)
	}
	if !strings.Contains(forged, canary) {
		t.Fatal("tampered file must now contain the raw canary")
	}

	// 启动校验必须拒绝被篡改的链。
	if _, err := audit.CreateFileAuditStorage(&audit.FileStorageConfig{
		LogDir: dir, FilePrefix: "decision", MaxFileSize: 1 << 20, MaxFiles: 2,
		VerifyOnStartup: true, FlushInterval: 60000,
	}); err == nil {
		t.Fatal("startup verification must reject a tampered chain")
	} else if !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected integrity failure, got: %v", err)
	}
}
