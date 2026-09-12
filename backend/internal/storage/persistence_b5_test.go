package storage

import (
	"fmt"
	"sync"
	"testing"
)

func TestSessionStorageRestartAndPagination(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	first, err := NewSessionStorage(path); if err != nil { t.Skipf("SQLite runtime unavailable: %v", err) }; defer first.Close()
	sess, err := first.CreateSession(CreateSessionInput{ID:"session-b5", Title:"B5"}); if err != nil { t.Fatal(err) }
	for i:=0;i<5;i++ { if _,err:=first.CreateMessage(CreateMessageInput{SessionID:sess.ID,Role:RoleUser,Content:fmt.Sprintf("m-%d",i)});err!=nil{t.Fatal(err)} }
	if got,err:=first.GetSessionMessages(sess.ID,&QueryOptions{Limit:2,Offset:2});err!=nil||len(got)!=2{t.Fatalf("pagination got=%d err=%v",len(got),err)}
	if err:=first.Close();err!=nil{t.Fatal(err)}
	second, err := NewSessionStorage(path); if err != nil { t.Fatal(err) }; defer second.Close()
	loaded, err := second.GetSessionWithMessages(sess.ID); if err != nil { t.Fatal(err) }
	if loaded == nil || len(loaded.Messages) != 5 { t.Fatalf("restart lost messages: %#v", loaded) }
}

func TestSessionStorageConcurrentMessages(t *testing.T) {
	store, err := NewSessionStorage(t.TempDir()+"/sessions.db"); if err != nil { t.Skipf("SQLite runtime unavailable: %v",err) }; defer store.Close()
	sess, err := store.CreateSession(CreateSessionInput{Title:"concurrent"}); if err != nil { t.Fatal(err) }
	var wg sync.WaitGroup; for i:=0;i<16;i++ { wg.Add(1); go func(i int){ defer wg.Done(); _,_ = store.CreateMessage(CreateMessageInput{SessionID:sess.ID,Role:RoleAssistant,Content:fmt.Sprintf("%d",i)}) }(i) }; wg.Wait()
	items, err := store.GetSessionMessages(sess.ID,nil); if err != nil { t.Fatal(err) }; if len(items) != 16 { t.Fatalf("expected 16 messages, got %d",len(items)) }
}
