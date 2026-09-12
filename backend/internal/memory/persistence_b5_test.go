package memory

import (
	"context"
	"testing"
)

func TestSQLiteMemoryRestartPagination(t *testing.T) {
	path := t.TempDir()+"/memory.db"
	first, err := NewSQLiteService(path); if err != nil { t.Skipf("SQLite runtime unavailable: %v",err) }
	ctx := context.Background()
	for i:=0;i<5;i++ { if _,err:=first.Create(ctx,&MemoryItemCreateRequest{Content:"memory",SessionID:"s"});err!=nil{t.Fatal(err)} }
	page,err:=first.List(ctx,&MemoryListOptions{SessionID:"s",Limit:2,Offset:2});if err!=nil||len(page.Items)!=2{t.Fatalf("page=%#v err=%v",page,err)}; if err:=first.Close();err!=nil{t.Fatal(err)}
	second,err:=NewSQLiteService(path);if err!=nil{t.Fatal(err)};defer second.Close();resp,err:=second.List(ctx,&MemoryListOptions{SessionID:"s",Limit:10});if err!=nil||resp.Total!=5{t.Fatalf("restart total=%d err=%v",resp.Total,err)}
}
