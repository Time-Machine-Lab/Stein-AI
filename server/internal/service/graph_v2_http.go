package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
)

type graphHTTPEffectV2 struct {
	WorkspaceID, RunID, NodeRunID, ClaimToken string
	Config                                    graph.HTTPConfig
	Headers                                   map[string]string
	Body                                      json.RawMessage
	InvocationID                              string
}

func (s *GraphService) prepareHTTPV2(run *GraphRunRecord, index int, node graph.Node, evaluation graph.EvaluationContext) (*graphHTTPEffectV2, error) {
	if s.ExecuteHTTP == nil {
		return nil, graphError("capability_missing", "HTTP executor unavailable")
	}
	n := &run.Nodes[index]
	headers := map[string]string{}
	var body json.RawMessage
	if n.Effect != nil {
		headers = n.Effect.Headers
		body = n.Effect.Body
		if n.Effect.Attempts >= 3 {
			return nil, graphError("budget_exhausted", "HTTP attempt budget exhausted")
		}
	} else {
		for key, b := range node.HTTP.Headers {
			raw, err := graph.ResolveBinding(b, evaluation)
			if err != nil {
				return nil, err
			}
			var value string
			if err = json.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("HTTP header %s must resolve to a string", key)
			}
			headers[key] = value
		}
		if node.HTTP.Body != nil {
			var err error
			body, err = graph.ResolveBinding(*node.HTTP.Body, evaluation)
			if err != nil {
				return nil, err
			}
		}
	}
	extra := len(body) + 4096
	for key, value := range headers {
		extra += len(key) + len(value)
	}
	if !graphPayloadFitsV2(*run, extra) {
		n.State = "failed"
		graphTerminateV2(run, "failed", "storage_budget_exhausted")
		return nil, nil
	}
	s.effectOnce.Do(func() { s.effectSlots = make(chan struct{}, 32) })
	select {
	case s.effectSlots <- struct{}{}:
	default:
		return nil, nil
	}
	count := 1
	if n.Effect != nil {
		count = n.Effect.Attempts + 1
	}
	now := time.Now().UTC()
	claim := uuid.NewString()
	n.Effect = &GraphEffectRecord{InvocationID: n.ID, ClaimToken: claim, State: "dispatched", LeaseUntil: now.Add(time.Duration(node.HTTP.TimeoutSeconds+30) * time.Second), Attempts: count, Headers: headers, Body: body}
	n.State = "active"
	n.BlockReason = ""
	n.MaxAttempts = 3
	if n.StartedAt == nil {
		n.StartedAt = &now
	}
	graphEvent(run, "http.dispatched", "", n.InstanceKey)
	return &graphHTTPEffectV2{WorkspaceID: run.WorkspaceID, RunID: run.ID, NodeRunID: n.ID, ClaimToken: claim, Config: *node.HTTP, Headers: headers, Body: body, InvocationID: n.ID}, nil
}

func (s *GraphService) executeHTTPV2(ctx context.Context, effect graphHTTPEffectV2) {
	defer func() { <-s.effectSlots }()
	// The dispatch is already committed. A completed API request must not cancel
	// its asynchronous effect; the callback has its own bounded lifetime.
	effectCtx, cancelEffect := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(effect.Config.TimeoutSeconds+5)*time.Second)
	defer cancelEffect()
	output, executeErr := s.ExecuteHTTP(effectCtx, effect.Config, effect.Headers, effect.Body, effect.InvocationID)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	tx, err := s.TxStarter.Begin(writeCtx)
	if err != nil {
		return
	}
	defer tx.Rollback(writeCtx)
	run, err := graphLoadRun(writeCtx, tx, effect.WorkspaceID, effect.RunID, true)
	if err != nil {
		return
	}
	index := graphNodeIndexV2(run, effect.NodeRunID)
	if index < 0 {
		return
	}
	n := &run.Nodes[index]
	if n.Effect == nil || n.Effect.ClaimToken != effect.ClaimToken || n.Effect.State != "dispatched" {
		return
	}
	n.Effect.State = "completed"
	if run.ControlState == "cancel_requested" || graphTerminal(run.State) {
		n.State = "cancelled"
		now := time.Now().UTC()
		n.FinishedAt = &now
	} else if executeErr != nil {
		if (effect.Config.Method == "GET" || effect.Config.Method == "HEAD") && n.Effect.Attempts < 3 {
			n.State = "ready"
			n.BlockReason = ""
			at := time.Now().UTC().Add(time.Duration(n.Effect.Attempts) * time.Second)
			n.NextAttemptAt = &at
			n.Effect.State = "retryable"
		} else {
			n.Effect.State = "unknown"
			graphBlockNodeV2(&run, index, "effect_outcome_unknown", executeErr)
		}
	} else if !graphPayloadFitsV2(run, len(output)+2048) {
		n.State = "failed"
		graphTerminateV2(&run, "failed", "storage_budget_exhausted")
	} else {
		version, err := graphLoadVersion(writeCtx, tx, run.WorkspaceID, run.VersionID)
		if err != nil {
			return
		}
		definition, err := graphNodeDefinitionV2(run, version.Definition, *n)
		if err != nil {
			return
		}
		if err = graph.ValidateJSON(definition.OutputSchema, output); err != nil {
			graphBlockNodeV2(&run, index, "output_invalid", err)
		} else {
			n.Output = output
			graphSucceedNodeV2(n)
			graphEvent(&run, "http.completed", "", n.InstanceKey)
		}
	}
	graphAggregateStateV2(&run)
	if err = graphOperation(writeCtx, tx, run, "http:"+effect.ClaimToken, "reconcile"); err != nil {
		return
	}
	if err = graphPersistRun(writeCtx, tx, &run); err != nil {
		return
	}
	if err = tx.Commit(writeCtx); err != nil {
		return
	}
	if s.NotifyRun != nil {
		s.NotifyRun(writeCtx, run)
	}
}
