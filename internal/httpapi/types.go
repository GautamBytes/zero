package httpapi

import (
	"context"
	"time"

	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/sessions"
	"github.com/Gitlawb/zero/internal/streamjson"
)

const defaultMaxFileBytes int64 = 1 << 20
const defaultMaxRequestBytes int64 = 32 << 20

type Runner interface {
	Run(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error)
}

type RunnerFunc func(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error)

func (fn RunnerFunc) Run(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error) {
	return fn(ctx, request, hooks)
}

type RunRequest struct {
	RunID           string                  `json:"runId"`
	SessionID       string                  `json:"sessionId"`
	Cwd             string                  `json:"cwd"`
	Content         string                  `json:"content"`
	Model           string                  `json:"model,omitempty"`
	ReasoningEffort string                  `json:"reasoningEffort,omitempty"`
	PermissionMode  string                  `json:"permissionMode,omitempty"`
	Autonomy        string                  `json:"autonomy,omitempty"`
	Images          []streamjson.InputImage `json:"images,omitempty"`
	Async           bool                    `json:"async,omitempty"`
	Store           *sessions.Store         `json:"-"`
}

type RunHooks struct {
	Emit                func(streamjson.Event)
	OnPermissionRequest func(context.Context, agent.PermissionRequest) (agent.PermissionDecision, error)
	OnAskUser           func(context.Context, agent.AskUserRequest) (agent.AskUserResponse, error)
}

type RunResult struct {
	RunID       string `json:"runId"`
	SessionID   string `json:"sessionId"`
	FinalAnswer string `json:"finalAnswer,omitempty"`
	Status      string `json:"status"`
	ExitCode    int    `json:"exitCode"`
}

type ConfigSnapshot struct {
	Version string `json:"version"`
	Cwd     string `json:"cwd"`
	Config  any    `json:"config,omitempty"`
}

type ProviderSnapshot struct {
	ActiveProvider string `json:"activeProvider,omitempty"`
	Model          string `json:"model,omitempty"`
	Providers      any    `json:"providers,omitempty"`
}

type ModelSnapshot struct {
	Models any `json:"models"`
}

type Options struct {
	Version         string
	Cwd             string
	Hostname        string
	Port            int
	Token           string
	NoAuth          bool
	CORS            []string
	Store           *sessions.Store
	Runner          Runner
	ConfigSnapshot  func(context.Context) (ConfigSnapshot, error)
	Provider        func(context.Context) (ProviderSnapshot, error)
	Models          func(context.Context) (ModelSnapshot, error)
	VCS             func(context.Context) (any, error)
	Now             func() time.Time
	MaxFileBytes    int64
	MaxRequestBytes int64
}

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
