package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/sessions"
)

func TestAuthCORSAndOpenAPI(t *testing.T) {
	server := New(Options{
		Version: "test",
		Cwd:     t.TempDir(),
		Token:   "secret",
		CORS:    []string{"https://app.example"},
		Runner:  RunnerFunc(successRunner),
		Store:   sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/global/health", nil)
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}
	var errorBody ErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &errorBody); err != nil {
		t.Fatal(err)
	}
	if errorBody.Error.Code != "unauthorized" {
		t.Fatalf("error code = %q", errorBody.Error.Code)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodOptions, "/session", nil)
	request.Header.Set("Origin", "https://app.example")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("allow origin = %q", got)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	request.Header.Set("Authorization", "Bearer secret")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("openapi status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", spec["openapi"])
	}
}

func TestCORSRejectsUntrustedActualRequest(t *testing.T) {
	server := New(Options{
		Cwd:    t.TempDir(),
		NoAuth: true,
		CORS:   []string{"https://app.example"},
		Runner: RunnerFunc(successRunner),
		Store:  sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/global/health", nil)
	request.Header.Set("Origin", "https://evil.example")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthFailsClosedWhenTokenMissing(t *testing.T) {
	server := New(Options{
		Cwd:    t.TempDir(),
		Runner: RunnerFunc(successRunner),
		Store:  sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
	})

	recorder := serveJSON(t, server, http.MethodGet, "/global/health", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "auth_misconfigured") {
		t.Fatalf("expected auth_misconfigured, got %s", recorder.Body.String())
	}
}

func TestJSONBodyLimitAndTrailingValue(t *testing.T) {
	server := New(Options{
		Cwd:             t.TempDir(),
		NoAuth:          true,
		MaxRequestBytes: 16,
		Runner:          RunnerFunc(successRunner),
		Store:           sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
	})

	recorder := serveJSON(t, server, http.MethodPost, "/session", `{"title":"this body is too large"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	server = New(Options{
		Cwd:    t.TempDir(),
		NoAuth: true,
		Runner: RunnerFunc(successRunner),
		Store:  sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
	})
	recorder = serveJSON(t, server, http.MethodPost, "/session", `{"title":"ok"} {"title":"extra"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("trailing status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"title":"text"}`))
	request.Header.Set("Content-Type", "text/plain")
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content-type status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionRoutes(t *testing.T) {
	store := sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()})
	server := New(Options{
		Cwd:    t.TempDir(),
		NoAuth: true,
		Store:  store,
		Runner: RunnerFunc(successRunner),
	})

	recorder := serveJSON(t, server, http.MethodPost, "/session", `{"sessionId":"s1","title":"One"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := store.AppendEvent("s1", sessions.AppendEventInput{Type: sessions.EventMessage, Payload: map[string]any{"role": "user", "content": "hello"}}); err != nil {
		t.Fatal(err)
	}
	recorder = serveJSON(t, server, http.MethodPost, "/session/s1/fork", `{"sessionId":"s2"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("fork status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodPatch, "/session/s2", `{"title":"Two"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodGet, "/session/s2/lineage", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("lineage status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Lineage []sessions.Metadata `json:"lineage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Lineage) != 2 || body.Lineage[0].SessionID != "s1" || body.Lineage[1].SessionID != "s2" {
		t.Fatalf("unexpected lineage: %+v", body.Lineage)
	}
}

func TestDuplicateRunReturnsConflict(t *testing.T) {
	store := sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()})
	if _, err := store.Create(sessions.CreateInput{SessionID: "s1", Title: "One"}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	server := New(Options{
		Cwd:    t.TempDir(),
		NoAuth: true,
		Store:  store,
		Runner: RunnerFunc(func(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error) {
			select {
			case <-ctx.Done():
				return RunResult{}, ctx.Err()
			case <-release:
				return RunResult{FinalAnswer: "done"}, nil
			}
		}),
	})
	recorder := serveJSON(t, server, http.MethodPost, "/session/s1/prompt_async", `{"content":"one"}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("async status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodPost, "/session/s1/message", `{"content":"two"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	close(release)
}

func TestPermissionAndAskBrokersBlockUntilHTTPAnswer(t *testing.T) {
	store := sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()})
	if _, err := store.Create(sessions.CreateInput{SessionID: "s1", Title: "One"}); err != nil {
		t.Fatal(err)
	}
	server := New(Options{
		Cwd:    t.TempDir(),
		NoAuth: true,
		Store:  store,
		Runner: RunnerFunc(func(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error) {
			decision, err := hooks.OnPermissionRequest(ctx, agent.PermissionRequest{
				ToolCallID:     "raw-tool-call-id",
				ToolName:       "bash",
				Action:         agent.PermissionActionPrompt,
				Permission:     "shell",
				PermissionMode: agent.PermissionModeAsk,
				SideEffect:     "shell",
				Reason:         "test",
			})
			if err != nil {
				return RunResult{}, err
			}
			answer, err := hooks.OnAskUser(ctx, agent.AskUserRequest{
				ToolCallID: "ask-tool-call-id",
				Questions:  []agent.AskUserQuestion{{Question: "Continue?"}},
			})
			if err != nil {
				return RunResult{}, err
			}
			return RunResult{FinalAnswer: string(decision.Action) + ":" + strings.Join(answer.Answers, ",")}, nil
		}),
	})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	resultCh := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Post(httpServer.URL+"/session/s1/message", "application/json", strings.NewReader(`{"content":"go"}`))
		if err != nil {
			t.Errorf("post message: %v", err)
			return
		}
		resultCh <- resp
	}()

	permissionID := waitForPendingPermission(t, server)
	resp, err := http.Post(httpServer.URL+"/session/s1/permissions/"+permissionID, "application/json", strings.NewReader(`{"action":"allow","reason":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permission status = %d", resp.StatusCode)
	}
	resp, err = http.Post(httpServer.URL+"/session/s1/permissions/"+permissionID, "application/json", strings.NewReader(`{"action":"deny"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("duplicate permission status = %d", resp.StatusCode)
	}

	askID := waitForPendingAsk(t, server)
	resp, err = http.Post(httpServer.URL+"/session/s1/ask/"+askID, "application/json", strings.NewReader(`{"answers":["yes"]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask status = %d", resp.StatusCode)
	}
	resp, err = http.Post(httpServer.URL+"/session/s1/ask/"+askID, "application/json", strings.NewReader(`{"answers":["late"]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("duplicate ask status = %d", resp.StatusCode)
	}

	select {
	case resp := <-resultCh:
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("message status = %d; body=%s", resp.StatusCode, string(data))
		}
		if !bytes.Contains(data, []byte(`"finalAnswer":"allow:yes"`)) {
			t.Fatalf("unexpected result: %s", string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message did not complete")
	}
}

func TestFileRoutesAreConfinedToWorkspace(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "hello.txt"), []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(cwd, "outside-link.txt"))
	server := New(Options{
		Cwd:    cwd,
		NoAuth: true,
		Store:  sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()}),
		Runner: RunnerFunc(successRunner),
	})

	recorder := serveJSON(t, server, http.MethodGet, "/file/content?path=hello.txt", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("content status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodGet, "/find?pattern=world", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "hello.txt") {
		t.Fatalf("find status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodGet, "/file/content?path=../outside.txt", "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("outside status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = serveJSON(t, server, http.MethodGet, "/file/content?path=outside-link.txt", "")
	if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusNotFound {
		t.Fatalf("symlink status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
}

func serveJSON(t *testing.T, handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func waitForPendingPermission(t *testing.T, server *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		server.permissions.mu.Lock()
		for id := range server.permissions.pending {
			server.permissions.mu.Unlock()
			return id
		}
		server.permissions.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("permission request was not pending")
	return ""
}

func waitForPendingAsk(t *testing.T, server *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		server.asks.mu.Lock()
		for id := range server.asks.pending {
			server.asks.mu.Unlock()
			return id
		}
		server.asks.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ask request was not pending")
	return ""
}

func successRunner(ctx context.Context, request RunRequest, hooks RunHooks) (RunResult, error) {
	return RunResult{FinalAnswer: "ok"}, nil
}
