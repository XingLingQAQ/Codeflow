package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/gin-gonic/gin"
)

func TestAuditMutationsRecordsSuccessAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode);storage:=audit.NewMemoryStorage();svc:=audit.NewAuditService(storage);audit.SetAuditService(svc);t.Cleanup(func(){audit.SetAuditService(nil)})
	r:=gin.New();r.Use(Trace(),AuditMutations());r.POST("/api/v1/projects/:id",func(c *gin.Context){if c.Param("id")=="fail"{c.JSON(http.StatusConflict,gin.H{"error":"conflict"});return};c.Status(http.StatusCreated)})
	for _,id:=range []string{"ok","fail"}{w:=httptest.NewRecorder();req:=httptest.NewRequest(http.MethodPost,"/api/v1/projects/"+id,nil);r.ServeHTTP(w,req)}
	result,err:=svc.Query(context.Background(),&audit.AuditQuery{Limit:10});if err!=nil{t.Fatal(err)};if len(result.Entries)!=2{t.Fatalf("expected two baseline records, got %d",len(result.Entries))};var successes,failures int;for _,entry:=range result.Entries{if entry.Outcome==audit.OutcomeSuccess{successes++}else{failures++};if entry.Resource.Type!="projects"{t.Fatalf("unexpected resource: %#v",entry.Resource)}};if successes!=1||failures!=1{t.Fatalf("success=%d failure=%d",successes,failures)}
}
