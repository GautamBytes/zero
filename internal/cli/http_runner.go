package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/httpapi"
	"github.com/Gitlawb/zero/internal/mcp"
	"github.com/Gitlawb/zero/internal/modelregistry"
	"github.com/Gitlawb/zero/internal/sandbox"
	"github.com/Gitlawb/zero/internal/sessions"
	"github.com/Gitlawb/zero/internal/streamjson"
	"github.com/Gitlawb/zero/internal/tools"
	"github.com/Gitlawb/zero/internal/usage"
)

type httpRunner struct {
	workspaceRoot string
	deps          appDeps
	stderr        io.Writer
}

func newHTTPRunner(workspaceRoot string, deps appDeps, stderr io.Writer) httpapi.Runner {
	return &httpRunner{workspaceRoot: workspaceRoot, deps: deps, stderr: stderr}
}

func (runner *httpRunner) Run(ctx context.Context, request httpapi.RunRequest, hooks httpapi.RunHooks) (httpapi.RunResult, error) {
	modelRegistry, _ := modelregistry.DefaultRegistry()
	overrides := config.Overrides{}
	if request.Model != "" {
		resolvedModel, notice := resolveSelectedModel(modelRegistry, request.Model)
		overrides.Provider.Model = resolvedModel
		if notice != "" && runner.stderr != nil {
			_, _ = fmt.Fprintln(runner.stderr, notice)
		}
	}
	resolved, err := runner.deps.resolveConfig(runner.workspaceRoot, overrides)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	if !config.HasProviderProfile(resolved.Provider) {
		return httpapi.RunResult{}, fmt.Errorf("no provider configured")
	}

	permissionMode, err := resolveHTTPPermissionMode(request)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	runReasoningEffort := strings.TrimSpace(request.ReasoningEffort)
	if runReasoningEffort != "" {
		if notice := reasoningEffortNotice(modelRegistry, resolved.Provider.Model, runReasoningEffort); notice != "" && runner.stderr != nil {
			_, _ = fmt.Fprintln(runner.stderr, notice)
		}
	}
	forwardEffort := forwardedReasoningEffort(modelRegistry, resolved.Provider.Model, runReasoningEffort)

	execScope, err := sandbox.NewScope(runner.workspaceRoot, resolved.Sandbox.AdditionalWriteRoots)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	registry := newCoreRegistryScoped(runner.workspaceRoot, execScope)
	registerLocalControlTools(registry, runner.workspaceRoot, resolved.LocalControl)
	var specialistRuntime *agentToolRuntime
	if shouldRegisterHTTPSpecialistTools(request, permissionMode) {
		specialistRuntime, err = registerSpecialistTools(registry, runner.workspaceRoot, resolved.Swarm.MaxTeamSize)
		if err != nil {
			return httpapi.RunResult{}, err
		}
	}
	defer closeSpecialistRuntime(runner.stderr, specialistRuntime)

	trustRoot := runner.workspaceRoot
	mcpRuntime, mcpSkip, err := registerMCPToolsForWorkspace(ctx, runner.workspaceRoot, registry, runner.deps, httpMCPAutonomy(request, permissionMode), trustRoot)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	defer closeMCPRuntime(runner.stderr, mcpRuntime)
	pluginActivation := activatePlugins(runner.workspaceRoot, registry, runner.deps, runner.stderr, trustRoot)
	registerToolSearchIfEligible(registry, resolved.Tools.DeferThreshold, permissionMode, nil, nil)

	sandboxEngine, err := buildExecSandboxEngine(runner.workspaceRoot, resolved, runner.deps, execScope)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	provider, err := buildProvider(resolved, runner.deps)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	runMetadata, err := resolveExecRunMetadata(resolved.Provider)
	if err != nil {
		return httpapi.RunResult{}, err
	}
	images, err := streamjson.ResolveImages([]streamjson.InputEvent{{
		SchemaVersion: streamjson.SchemaVersion,
		Type:          streamjson.InputMessage,
		Role:          "user",
		Content:       streamJSONInputContent(request.Content),
		Images:        request.Images,
	}})
	if err != nil {
		return httpapi.RunResult{}, err
	}
	if len(images) > 0 && !modelregistry.SupportsVision(modelRegistry, resolved.Provider.Model) {
		images = nil
		hooks.Emit(streamjson.Event{
			Type:    streamjson.EventWarning,
			Message: fmt.Sprintf("Model %s does not support image input; ignoring image(s).", resolved.Provider.Model),
		})
	}

	preparedSession, err := sessions.PrepareExec(sessions.PrepareExecOptions{
		Store:    request.Store,
		Title:    createSessionTitle(request.Content),
		Cwd:      runner.workspaceRoot,
		ModelID:  resolved.Provider.Model,
		Provider: runMetadata.Provider,
		Resume:   request.SessionID,
	})
	if err != nil {
		return httpapi.RunResult{}, err
	}
	agentPrompt := sessions.FormatExecPrompt(request.Content, preparedSession)
	sessionRecorder := execSessionRecorder{prepared: preparedSession}
	defer sessionRecorder.warnIfRecordingFailed(runner.stderr)
	sessionRecorder.append(sessions.EventMessage, map[string]any{
		"role":    "user",
		"content": request.Content,
	})

	fileTracker := tools.NewFileTracker()
	_, fileDiagnostics, lspShutdown := newExecSelfCorrector(false, runner.workspaceRoot, request.Autonomy)
	defer lspShutdown()
	hookDispatcher, hookSkip := newHookDispatcherWithExtra(runner.workspaceRoot, pluginActivation.hooks, trustRoot)
	emitTrustNotice(runner.stderr, hookSkip, pluginActivation.trustSkip, mcpSkip)

	var streamedText strings.Builder
	result, err := agent.Run(ctx, agentPrompt, provider, agent.Options{
		MaxTurns:                resolved.MaxTurns,
		ContextWindow:           resolveAgentContextWindow(ctx, modelRegistry, resolved.Provider),
		DeferThreshold:          resolved.Tools.DeferThreshold,
		Specialists:             specialistRuntime.specialistInfos(),
		Skills:                  pluginActivation.skillInfos(runner.deps.skillsDir()),
		SessionID:               preparedSession.Session.SessionID,
		SessionTitle:            preparedSession.Session.Title,
		ProviderName:            resolved.Provider.Name,
		Model:                   resolved.Provider.Model,
		ReasoningEffort:         forwardEffort,
		Cwd:                     runner.workspaceRoot,
		Images:                  images,
		Registry:                registry,
		PermissionMode:          permissionMode,
		Autonomy:                request.Autonomy,
		Sandbox:                 sandboxEngine,
		FileTracker:             fileTracker,
		Hooks:                   hookDispatcher,
		FileDiagnostics:         fileDiagnostics,
		RequireCompletionSignal: true,
		OnText: func(delta string) {
			streamedText.WriteString(delta)
			hooks.Emit(streamjson.Event{Type: streamjson.EventText, Delta: delta})
		},
		OnReasoning: func(delta string) {
			hooks.Emit(streamjson.Event{Type: streamjson.EventReasoning, Delta: delta})
		},
		OnToolCall: func(call agent.ToolCall) {
			hooks.Emit(streamjson.Event{
				Type:       streamjson.EventToolCall,
				ID:         call.ID,
				Name:       call.Name,
				Args:       parseToolCallArgs(call.Arguments),
				SideEffect: streamJSONSideEffect(call.Name, registry),
			})
			sessionRecorder.append(sessions.EventToolCall, map[string]any{
				"id":        call.ID,
				"name":      call.Name,
				"arguments": call.Arguments,
			})
			if checkpoint, ok := sessionRecorder.captureCheckpoint(runner.workspaceRoot, call); ok {
				hooks.Emit(streamJSONCheckpointEvent(checkpoint))
			}
		},
		OnPermissionRequest: hooks.OnPermissionRequest,
		OnAskUser:           hooks.OnAskUser,
		OnPermission: func(event agent.PermissionEvent) {
			emitHTTPPermissionEvent(hooks, event)
			sessionRecorder.append(sessionPermissionEventType(event), event)
		},
		OnToolResult: func(result agent.ToolResult) {
			emitHTTPToolResult(hooks, result)
			payload := map[string]any{
				"toolCallId": result.ToolCallID,
				"name":       result.Name,
				"status":     string(result.Status),
				"output":     result.Output,
			}
			if len(result.Meta) > 0 {
				payload["meta"] = result.Meta
			}
			if result.Redacted {
				payload["redacted"] = true
			}
			if len(result.ChangedFiles) > 0 {
				payload["changedFiles"] = result.ChangedFiles
			}
			sessionRecorder.append(sessions.EventToolResult, payload)
		},
		OnUsage: func(u agent.Usage) {
			promptTokens := u.EffectiveInputTokens()
			completionTokens := u.EffectiveOutputTokens()
			totalTokens := u.TotalTokens()
			hooks.Emit(streamjson.Event{
				Type:             streamjson.EventUsage,
				PromptTokens:     &promptTokens,
				CompletionTokens: &completionTokens,
				TotalTokens:      &totalTokens,
			})
			sessionRecorder.append(sessions.EventUsage, usage.EventUsagePayload(u))
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			sessionRecorder.append(sessions.EventError, map[string]any{"message": "interrupted"})
		} else {
			sessionRecorder.append(sessions.EventError, map[string]any{"message": err.Error()})
		}
		return httpapi.RunResult{}, err
	}
	sessionRecorder.append(sessions.EventMessage, map[string]any{
		"role":    "assistant",
		"content": result.FinalAnswer,
	})
	if notice := result.TruncationNotice(); notice != "" {
		hooks.Emit(streamjson.Event{Type: streamjson.EventWarning, Message: notice})
	}
	status := "success"
	exitCode := exitSuccess
	if result.Incomplete {
		status = "incomplete"
		exitCode = exitIncomplete
		reason := result.IncompleteReason
		if reason == "" {
			reason = "run stopped with work unfinished"
		}
		sessionRecorder.append(sessions.EventError, map[string]any{"message": "incomplete: " + reason})
		hooks.Emit(streamjson.Event{
			Type:        streamjson.EventError,
			Code:        "incomplete",
			Message:     reason,
			Recoverable: httpBoolPtr(false),
		})
	}
	final := result.FinalAnswer
	if final == "" {
		final = streamedText.String()
	}
	return httpapi.RunResult{
		RunID:       request.RunID,
		SessionID:   request.SessionID,
		FinalAnswer: final,
		Status:      status,
		ExitCode:    exitCode,
	}, nil
}

func resolveHTTPPermissionMode(request httpapi.RunRequest) (agent.PermissionMode, error) {
	switch agent.PermissionMode(strings.TrimSpace(request.PermissionMode)) {
	case "":
		return resolveExecPermissionMode(execOptions{autonomy: request.Autonomy})
	case agent.PermissionModeAuto, agent.PermissionModeAsk, agent.PermissionModeUnsafe:
		return agent.PermissionMode(strings.TrimSpace(request.PermissionMode)), nil
	default:
		return "", execUsageError{fmt.Sprintf("Invalid permission mode %q. Expected auto, ask, or unsafe.", request.PermissionMode)}
	}
}

func httpMCPAutonomy(request httpapi.RunRequest, mode agent.PermissionMode) mcp.PermissionAutonomy {
	if mode == agent.PermissionModeUnsafe {
		return mcp.AutonomyHigh
	}
	switch strings.ToLower(strings.TrimSpace(request.Autonomy)) {
	case "high":
		return mcp.AutonomyHigh
	case "medium":
		return mcp.AutonomyMedium
	default:
		return mcp.AutonomyLow
	}
}

func shouldRegisterHTTPSpecialistTools(request httpapi.RunRequest, mode agent.PermissionMode) bool {
	return shouldRegisterExecSpecialistTools(execOptions{
		autonomy:              request.Autonomy,
		skipPermissionsUnsafe: mode == agent.PermissionModeUnsafe,
	})
}

func emitHTTPPermissionEvent(hooks httpapi.RunHooks, event agent.PermissionEvent) {
	risk := event.Risk
	permissionGranted := event.PermissionGranted
	hooks.Emit(streamjson.Event{
		Type:              streamJSONPermissionEventType(event),
		ID:                event.ToolCallID,
		Name:              event.ToolName,
		Action:            string(event.Action),
		Permission:        event.Permission,
		PermissionGranted: &permissionGranted,
		PermissionMode:    string(event.PermissionMode),
		Autonomy:          event.Autonomy,
		SideEffect:        event.SideEffect,
		Reason:            event.Reason,
		DecisionReason:    event.DecisionReason,
		Risk:              &risk,
		Block:             event.Block,
		GrantMatched:      event.GrantMatched,
		Grant:             event.Grant,
	})
}

func emitHTTPToolResult(hooks httpapi.RunHooks, result agent.ToolResult) {
	output, truncated := truncateForStreamJSONOutput(result.Output)
	event := streamjson.Event{
		Type:         streamjson.EventToolResult,
		ID:           result.ToolCallID,
		Name:         result.Name,
		Status:       string(result.Status),
		Output:       output,
		Truncated:    &truncated,
		ChangedFiles: result.ChangedFiles,
		Meta:         result.Meta,
	}
	if result.Redacted {
		redacted := true
		event.Redacted = &redacted
	}
	if result.Display.Summary != "" || result.Display.Kind != "" {
		event.Display = &streamjson.Display{Summary: result.Display.Summary, Kind: result.Display.Kind}
	}
	hooks.Emit(event)
}

func httpBoolPtr(value bool) *bool {
	return &value
}

func streamJSONInputContent(content string) string {
	if strings.TrimSpace(content) != "" {
		return content
	}
	return "image input"
}

func streamJSONCheckpointEvent(event sessions.Event) streamjson.Event {
	var payload sessions.CheckpointPayload
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	files := make([]string, 0, len(payload.Files))
	for _, file := range payload.Files {
		files = append(files, file.Path)
	}
	return streamjson.Event{
		Type:       streamjson.EventCheckpoint,
		Checkpoint: &streamjson.CheckpointInfo{Sequence: event.Sequence, Tool: payload.Tool, Files: files},
	}
}
