package agent

import (
	"context"
	"testing"
)

func TestSQLiteAgentServiceRestartConversationTrace(t *testing.T) {
	path := t.TempDir()+"/sessions.db"
	first,err:=NewSQLiteAgentService(path);if err!=nil{t.Skipf("SQLite runtime unavailable: %v",err)}
	first.RegisterAgent(&Agent{ID:"agent-b5",Name:"B5",Role:RoleCoder,SessionID:"session-b5"})
	tid:=first.StartTrace("session-b5","agent-b5","tool",map[string]interface{}{"ok":true}); first.EndTrace(tid,"done","completed"); if err:=first.Close();err!=nil{t.Fatal(err)}
	second,err:=NewSQLiteAgentService(path);if err!=nil{t.Fatal(err)};defer second.Close();trace,err:=second.GetConversationTrace(context.Background(),"session-b5");if err!=nil{t.Fatal(err)};if trace==nil||trace.Trace==nil||trace.Trace.Output!="done"{t.Fatalf("trace not restored: %#v",trace)}
}
