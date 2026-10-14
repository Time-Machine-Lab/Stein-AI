package service

import (
 "context"
 "encoding/json"
 "sync"
 "testing"
 "time"

 "github.com/multica-ai/multica/server/internal/graph"
)

func graphHTTPStopFenceDefinition(delayed bool) graph.Definition {
 schema:=json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
 body:=graph.Definition{SchemaVersion:2,Name:"Bounded body",InputSchema:schema,OutputSchema:schema,Nodes:[]graph.Node{{Key:"start",Type:graph.NodeTypeStart},{Key:"end",Type:graph.NodeTypeEnd}},Edges:[]graph.Edge{{From:"start",To:"end"}}}
 if delayed {body.Nodes=append(body.Nodes,graph.Node{Key:"wait",Type:graph.NodeTypeDelay,Delay:&graph.DelayConfig{Seconds:60}});body.Edges=[]graph.Edge{{From:"start",To:"wait"},{From:"wait",To:"end"}}}
 request:=graph.Node{Key:"new_post",Type:graph.NodeTypeHTTP,OutputSchema:schema,HTTP:&graph.HTTPConfig{Method:"POST",URL:"https://example.org/new",TimeoutSeconds:10,MaxResponseBytes:1024}}
 loop:=graph.Node{Key:"loop",Type:graph.NodeTypeLoop,OutputSchema:schema,Loop:&graph.LoopConfig{Body:body,MaxIterations:1,Condition:graph.Condition{Op:"eq",Left:&graph.InputBinding{Source:graph.BindingConstant,Value:json.RawMessage(`true`)},Right:&graph.InputBinding{Source:graph.BindingConstant,Value:json.RawMessage(`true`)}}}}
 d:=graph.Definition{SchemaVersion:2,Name:"Dispatch stop fence",InputSchema:json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"]}`),Nodes:[]graph.Node{{Key:"start",Type:graph.NodeTypeStart},{Key:"fork",Type:graph.NodeTypeParallel},request,loop,{Key:"join",Type:graph.NodeTypeJoin},{Key:"end",Type:graph.NodeTypeEnd}},Edges:[]graph.Edge{{From:"start",To:"fork"},{From:"fork",To:"new_post"},{From:"fork",To:"loop"},{From:"new_post",To:"join"},{From:"loop",To:"join"},{From:"join",To:"end"}}}
 if delayed {
  prior:=request;prior.Key="prior_post";copy:=*request.HTTP;copy.URL="https://example.org/prior";prior.HTTP=&copy
  d.Nodes=append(d.Nodes,prior,graph.Node{Key:"wait",Type:graph.NodeTypeDelay,Delay:&graph.DelayConfig{Seconds:60}})
  d.Edges[1].To="wait";d.Edges=append(d.Edges,graph.Edge{From:"wait",To:"new_post"},graph.Edge{From:"fork",To:"prior_post"},graph.Edge{From:"prior_post",To:"join"})
 }
 return d
}

func TestGraphV2PostgresStopBeforePreparedHTTPDispatch(t *testing.T) {
 f:=newGraphFixture(t);f=graphV2Publish(t,f,graphHTTPStopFenceDefinition(false))
 called:=make(chan struct{},1)
 f.svc.ExecuteHTTP=func(context.Context,graph.HTTPConfig,map[string]string,json.RawMessage,string)(json.RawMessage,error){called<-struct{}{};return json.RawMessage(`{}`),nil}
 run:=graphV2StartAny(t,f)
 select {case <-called:t.Fatal("POST was sent after the same scheduling transaction requested termination");case <-time.After(100*time.Millisecond):}
 if run.State!="failed" || run.TerminationReason!="iteration_budget_exhausted" {t.Fatalf("unstarted HTTP prevents terminal failure: %s %s",run.State,run.TerminationReason)}
 n:=run.Nodes[graphV2NodeIndex(t,run,"new_post")]
 if n.Effect==nil||n.Effect.State!="cancelled"||len(f.svc.effectSlots)!=0 {t.Fatalf("prepared effect/slot not cancelled: %+v slots=%d",n.Effect,len(f.svc.effectSlots))}
}

func TestGraphV2PostgresPreparedHTTPCancelPreservesDispatchedEffect(t *testing.T) {
 f:=newGraphFixture(t);f=graphV2Publish(t,f,graphHTTPStopFenceDefinition(true))
 started:=make(chan string,2);release:=make(chan struct{});var once sync.Once;defer once.Do(func(){close(release)})
 f.svc.ExecuteHTTP=func(ctx context.Context,config graph.HTTPConfig,_ map[string]string,_ json.RawMessage,_ string)(json.RawMessage,error){started<-config.URL;if config.URL=="https://example.org/prior" {select{case <-release:case <-ctx.Done():return nil,ctx.Err()}};return json.RawMessage(`{}`),nil}
 run:=graphV2StartAny(t,f)
 select{case url:=<-started:if url!="https://example.org/prior"{t.Fatal("unexpected initial effect")};case <-time.After(3*time.Second):t.Fatal("prior request not started")}
 past:=time.Now().Add(-time.Second);for i:=range run.Nodes {if run.Nodes[i].NodeType=="delay"{run.Nodes[i].WakeAt=&past}}
 tx,err:=f.pool.Begin(context.Background());if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if err=graphPersistRun(context.Background(),tx,&run);err!=nil{t.Fatal(err)};if err=tx.Commit(context.Background());err!=nil{t.Fatal(err)}
 run=f.reconcile(t,run)
 select {case url:=<-started:t.Fatalf("new request sent while stopping: %s",url);case <-time.After(100*time.Millisecond):}
 prior:=run.Nodes[graphV2NodeIndex(t,run,"prior_post")];pending:=run.Nodes[graphV2NodeIndex(t,run,"new_post")]
 if graphTerminal(run.State)||run.ControlState!="cancel_requested"||prior.Effect.State!="dispatched"||pending.Effect.State!="cancelled"||len(f.svc.effectSlots)!=1 {t.Fatalf("lost already-dispatched effect stop fence: state=%s control=%s prior=%+v pending=%+v slots=%d",run.State,run.ControlState,prior.Effect,pending.Effect,len(f.svc.effectSlots))}
 once.Do(func(){close(release)})
 deadline:=time.Now().Add(3*time.Second);for len(f.svc.effectSlots)>0&&time.Now().Before(deadline){time.Sleep(5*time.Millisecond)}
 if len(f.svc.effectSlots)!=0{t.Fatal("prior callback did not settle")}
 run=f.reconcile(t,run);if run.State!="failed"{t.Fatalf("run did not finish after actual prior callback: %s",run.State)}
}
