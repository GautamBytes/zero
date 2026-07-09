package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/streamjson"
)

type eventBroker struct {
	mu          sync.Mutex
	subscribers map[chan streamjson.Event]string
}

var controlEventSendTimeout = 5 * time.Second

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: map[chan streamjson.Event]string{}}
}

func (broker *eventBroker) subscribe(sessionID string) (chan streamjson.Event, func()) {
	ch := make(chan streamjson.Event, 64)
	broker.mu.Lock()
	broker.subscribers[ch] = strings.TrimSpace(sessionID)
	broker.mu.Unlock()
	return ch, func() {
		broker.mu.Lock()
		if _, ok := broker.subscribers[ch]; ok {
			delete(broker.subscribers, ch)
			close(ch)
		}
		broker.mu.Unlock()
	}
}

func (broker *eventBroker) publish(event streamjson.Event) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	for ch, sessionID := range broker.subscribers {
		if sessionID != "" && event.SessionID != "" && sessionID != event.SessionID {
			continue
		}
		if isBlockingControlEvent(event) {
			timer := time.NewTimer(controlEventSendTimeout)
			select {
			case ch <- event:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
				delete(broker.subscribers, ch)
				close(ch)
			}
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func isBlockingControlEvent(event streamjson.Event) bool {
	return event.Type == streamjson.EventPermissionRequest || event.Type == streamjson.EventType("ask_user_request")
}

type permissionBroker struct {
	mu      sync.Mutex
	pending map[string]pendingPermission
}

type pendingPermission struct {
	sessionID string
	ch        chan permissionResponse
}

type permissionResponse struct {
	decision agent.PermissionDecision
	err      error
}

func newPermissionBroker() *permissionBroker {
	return &permissionBroker{pending: map[string]pendingPermission{}}
}

func (broker *permissionBroker) request(ctx context.Context, sessionID string, req agent.PermissionRequest, publish func(streamjson.Event)) (agent.PermissionDecision, error) {
	id, err := newOpaqueID("perm")
	if err != nil {
		return agent.PermissionDecision{}, err
	}
	ch := make(chan permissionResponse, 1)
	broker.mu.Lock()
	broker.pending[id] = pendingPermission{sessionID: sessionID, ch: ch}
	broker.mu.Unlock()
	defer func() {
		broker.mu.Lock()
		delete(broker.pending, id)
		broker.mu.Unlock()
	}()

	risk := req.Risk
	publish(streamjson.Event{
		Type:           streamjson.EventPermissionRequest,
		SessionID:      sessionID,
		ID:             id,
		Name:           req.ToolName,
		Action:         string(req.Action),
		Permission:     req.Permission,
		PermissionMode: string(req.PermissionMode),
		Autonomy:       req.Autonomy,
		SideEffect:     req.SideEffect,
		Reason:         req.Reason,
		Risk:           &risk,
		Block:          req.Block,
		GrantMatched:   req.GrantMatched,
		Grant:          req.Grant,
		Args: map[string]any{
			"permissionId":       id,
			"toolCallId":         req.ToolCallID,
			"args":               req.Args,
			"scope":              req.Scope,
			"commandPrefix":      req.CommandPrefix,
			"availableDecisions": req.AvailableDecisions,
		},
	})

	select {
	case <-ctx.Done():
		return agent.PermissionDecision{}, ctx.Err()
	case response := <-ch:
		if response.err != nil {
			return agent.PermissionDecision{}, response.err
		}
		publish(streamjson.Event{
			Type:           streamjson.EventPermissionDecision,
			SessionID:      sessionID,
			ID:             id,
			Name:           req.ToolName,
			Action:         string(response.decision.Action),
			Permission:     req.Permission,
			PermissionMode: string(req.PermissionMode),
			Autonomy:       req.Autonomy,
			SideEffect:     req.SideEffect,
			Reason:         req.Reason,
			DecisionReason: response.decision.Reason,
		})
		return response.decision, nil
	}
}

func (broker *permissionBroker) respond(sessionID string, id string, decision agent.PermissionDecision) error {
	id = strings.TrimSpace(id)
	broker.mu.Lock()
	pending, ok := broker.pending[id]
	if !ok {
		broker.mu.Unlock()
		return notFoundError("permission_not_found", "permission request not found")
	}
	if pending.sessionID != sessionID {
		broker.mu.Unlock()
		return notFoundError("permission_not_found", "permission request not found")
	}
	delete(broker.pending, id)
	broker.mu.Unlock()
	pending.ch <- permissionResponse{decision: decision}
	return nil
}

type askBroker struct {
	mu      sync.Mutex
	pending map[string]pendingAsk
}

type pendingAsk struct {
	sessionID string
	ch        chan askResponse
}

type askResponse struct {
	response agent.AskUserResponse
	err      error
}

func newAskBroker() *askBroker {
	return &askBroker{pending: map[string]pendingAsk{}}
}

func (broker *askBroker) request(ctx context.Context, sessionID string, req agent.AskUserRequest, publish func(streamjson.Event)) (agent.AskUserResponse, error) {
	id, err := newOpaqueID("ask")
	if err != nil {
		return agent.AskUserResponse{}, err
	}
	ch := make(chan askResponse, 1)
	broker.mu.Lock()
	broker.pending[id] = pendingAsk{sessionID: sessionID, ch: ch}
	broker.mu.Unlock()
	defer func() {
		broker.mu.Lock()
		delete(broker.pending, id)
		broker.mu.Unlock()
	}()

	publish(streamjson.Event{
		Type:      streamjson.EventType("ask_user_request"),
		SessionID: sessionID,
		ID:        id,
		Name:      "ask_user",
		Args: map[string]any{
			"askId":      id,
			"toolCallId": req.ToolCallID,
			"header":     req.Header,
			"questions":  req.Questions,
		},
	})

	select {
	case <-ctx.Done():
		return agent.AskUserResponse{}, ctx.Err()
	case response := <-ch:
		if response.err != nil {
			return agent.AskUserResponse{}, response.err
		}
		publish(streamjson.Event{
			Type:      streamjson.EventType("ask_user_answer"),
			SessionID: sessionID,
			ID:        id,
			Name:      "ask_user",
			Args: map[string]any{
				"askId":   id,
				"answers": response.response.Answers,
			},
		})
		return response.response, nil
	}
}

func (broker *askBroker) respond(sessionID string, id string, response agent.AskUserResponse) error {
	id = strings.TrimSpace(id)
	broker.mu.Lock()
	pending, ok := broker.pending[id]
	if !ok {
		broker.mu.Unlock()
		return notFoundError("ask_not_found", "ask_user request not found")
	}
	if pending.sessionID != sessionID {
		broker.mu.Unlock()
		return notFoundError("ask_not_found", "ask_user request not found")
	}
	delete(broker.pending, id)
	broker.mu.Unlock()
	pending.ch <- askResponse{response: response}
	return nil
}

func serveSSE(w http.ResponseWriter, r *http.Request, broker *eventBroker) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	ch, unsubscribe := broker.subscribe(sessionID)
	defer unsubscribe()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, event)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event streamjson.Event) {
	data, err := streamjson.FormatEvent(event)
	if err != nil {
		raw, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			raw = []byte(`{"type":"error","message":"failed to encode event"}`)
		}
		data = string(raw)
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func newOpaqueID(prefix string) (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}
