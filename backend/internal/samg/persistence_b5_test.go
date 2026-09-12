package samg

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteSAMGRestartKeepsGraphAndAccess(t *testing.T) {
	path:=t.TempDir()+"/samg.db";ctx:=context.Background();first,err:=NewSQLiteSAMGService(path,nil);if err!=nil{t.Skipf("SQLite runtime unavailable: %v",err)}
	triple:=Triple{ID:"b5-triple",Subject:CreateNode("entity:a",EntityTypes.Class,"A"),Predicate:Predicates.Extends,Object:CreateNodeObject(CreateNode("entity:b",EntityTypes.Class,"B")),Confidence:.9,Timestamp:time.Now().UnixMilli()};if err:=first.AddTriples(ctx,[]Triple{triple});err!=nil{t.Fatal(err)};if err:=first.RecordAccess(ctx,"entity:a");err!=nil{t.Fatal(err)};if err:=first.Close();err!=nil{t.Fatal(err)}
	second,err:=NewSQLiteSAMGService(path,nil);if err!=nil{t.Fatal(err)};defer second.Close();loaded,err:=second.GetTriple(ctx,"b5-triple");if err!=nil||loaded==nil{t.Fatalf("graph lost err=%v",err)};top:=second.GetTopNodes(ctx,1);if len(top)!=1||top[0].AccessCount!=1{t.Fatalf("access metadata lost: %#v",top)}
}
