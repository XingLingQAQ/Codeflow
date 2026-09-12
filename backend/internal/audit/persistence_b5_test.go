package audit

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAuditRotationRestartContinuity(t *testing.T) {
	dir:=t.TempDir();cfg:=&FileStorageConfig{LogDir:dir,FilePrefix:"b5",MaxFileSize:300,MaxFiles:2,VerifyOnStartup:true,FlushInterval:60000};store,err:=CreateFileAuditStorage(cfg);if err!=nil{t.Fatal(err)};svc:=NewAuditService(store);ctx:=context.Background()
	for i:=0;i<12;i++ { if err:=svc.Log(ctx,&AuditLogEntry{EventType:EventModify,Severity:SeverityInfo,Actor:AuditActor{ID:"user",Type:"user"},Resource:AuditResource{Type:"test",ID:"resource"},Action:"mutate",Outcome:OutcomeSuccess});err!=nil{t.Fatal(err)};store.mu.Lock();err=store.flushLocked();store.mu.Unlock();if err!=nil{t.Fatal(err)} }
	result,err:=svc.VerifyChain(ctx);if err!=nil||!result.Valid{t.Fatalf("rotation chain invalid: %#v err=%v",result,err)};if err:=svc.Close();err!=nil{t.Fatal(err)}
	restarted,err:=CreateFileAuditStorage(cfg);if err!=nil{t.Fatalf("restart failed: %v",err)};defer restarted.Close();result,err=restarted.VerifyHashChain(ctx);if err!=nil||!result.Valid{t.Fatalf("restart chain invalid: %#v err=%v",result,err)}
}

func TestAuditStartupFailsClosedOnCorruption(t *testing.T) {
	dir:=t.TempDir();cfg:=&FileStorageConfig{LogDir:dir,FilePrefix:"b5",MaxFileSize:1<<20,MaxFiles:3,VerifyOnStartup:true,FlushInterval:60000};store,err:=CreateFileAuditStorage(cfg);if err!=nil{t.Fatal(err)};svc:=NewAuditService(store);if err:=svc.Log(context.Background(),&AuditLogEntry{EventType:EventSecurity,Severity:SeverityInfo,Actor:AuditActor{ID:"system",Type:"system"},Resource:AuditResource{Type:"test",ID:"1"},Action:"write",Outcome:OutcomeSuccess});err!=nil{t.Fatal(err)};if err:=svc.Close();err!=nil{t.Fatal(err)}
	files,err:=store.getLogFiles();if err!=nil||len(files)==0{t.Fatalf("logs missing: %v",err)};raw,err:=os.ReadFile(files[0]);if err!=nil{t.Fatal(err)};if err:=os.WriteFile(files[0],[]byte(strings.Replace(string(raw),"\"action\":\"write\"","\"action\":\"tampered\"",1)),0o600);err!=nil{t.Fatal(err)}
	corrupt:=NewFileAuditStorage(cfg);if err:=corrupt.Initialize();err==nil{_ = corrupt.Close();t.Fatal("expected corrupt audit chain to fail startup")}
}
