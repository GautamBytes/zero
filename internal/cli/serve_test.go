package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Gitlawb/zero/internal/httpapi"
)

func TestRunServeMCPListsReadOnlyToolsByDefault(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDeps([]string{"serve", "--mcp"}, &stdout, &stderr, appDeps{
		getwd: func() (string, error) {
			return t.TempDir(), nil
		},
		stdin: bytes.NewReader(serveMCPInput(t)),
	})

	if exitCode != exitSuccess {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"read_file", "list_directory", "glob", "grep"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected MCP output to contain %q, got %q", want, output)
		}
	}
	for _, unwanted := range []string{"write_file", "apply_patch", "bash", "web_fetch"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("did not expect default MCP output to contain %q: %q", unwanted, output)
		}
	}
}

func TestRunServeMCPAllowsUnsafeToolsWithExplicitFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDeps([]string{"serve", "--mcp", "--allow-unsafe-tools"}, &stdout, &stderr, appDeps{
		getwd: func() (string, error) {
			return t.TempDir(), nil
		},
		stdin: bytes.NewReader(serveMCPInput(t)),
	})

	if exitCode != exitSuccess {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"read_file", "write_file", "apply_patch", "bash", "web_fetch"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected unsafe MCP output to contain %q, got %q", want, output)
		}
	}
	if !strings.Contains(stderr.String(), "Unsafe MCP server tools enabled") {
		t.Fatalf("expected unsafe warning on stderr, got %q", stderr.String())
	}
}

func TestRunServeRequiresMCPMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDeps([]string{"serve"}, &stdout, &stderr, appDeps{})

	if exitCode != exitUsage {
		t.Fatalf("expected exit code %d, got %d", exitUsage, exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "serve requires exactly one of --mcp or --http") {
		t.Fatalf("expected usage error, got %q", stderr.String())
	}
}

func TestParseServeHTTPFlags(t *testing.T) {
	options, help, err := parseServeArgs([]string{
		"--http",
		"--hostname", "localhost",
		"--port=4100",
		"--cors", "https://app.example",
		"--cors=https://admin.example",
		"--auth-token", "token",
		"-C", "/tmp/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if help {
		t.Fatal("unexpected help")
	}
	if !options.http || options.mcp {
		t.Fatalf("mode flags = http:%v mcp:%v", options.http, options.mcp)
	}
	if options.hostname != "localhost" || options.port != 4100 || options.authToken != "token" || options.cwd != "/tmp/work" {
		t.Fatalf("unexpected options: %+v", options)
	}
	if got := strings.Join(options.cors, ","); got != "https://app.example,https://admin.example" {
		t.Fatalf("cors = %q", got)
	}
}

func TestParseServeRejectsInvalidHTTPPort(t *testing.T) {
	_, _, err := parseServeArgs([]string{"--http", "--port", "70000"})
	if err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestRunServeRejectsMixedServeFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "mcp with http flag", args: []string{"serve", "--mcp", "--port", "4100"}, want: "HTTP flags require --http"},
		{name: "http with unsafe mcp flag", args: []string{"serve", "--http", "--allow-unsafe-tools"}, want: "--allow-unsafe-tools is only supported with --mcp"},
		{name: "http no auth with token", args: []string{"serve", "--http", "--no-auth", "--auth-token", "secret"}, want: "use either --no-auth or --auth-token, not both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runWithDeps(tt.args, &stdout, &stderr, appDeps{
				getwd: func() (string, error) {
					return t.TempDir(), nil
				},
			})
			if exitCode != exitUsage {
				t.Fatalf("exit code = %d, want %d; stderr=%q", exitCode, exitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestResolveServeHTTPTokenUsesEnvironmentWithoutGenerating(t *testing.T) {
	t.Setenv("ZERO_SERVER_TOKEN", "env-token")
	token, generated, err := resolveServeHTTPToken(serveOptions{http: true})
	if err != nil {
		t.Fatal(err)
	}
	if token != "env-token" || generated {
		t.Fatalf("token=%q generated=%v", token, generated)
	}
}

func TestResolveHTTPPermissionModeRejectsSpecDraft(t *testing.T) {
	_, err := resolveHTTPPermissionMode(httpapi.RunRequest{PermissionMode: "spec-draft"})
	if err == nil {
		t.Fatal("expected spec-draft to be rejected")
	}
}

func TestHTTPSpecialistToolsUseExecAutonomyGate(t *testing.T) {
	if shouldRegisterHTTPSpecialistTools(httpapi.RunRequest{}, "") {
		t.Fatal("default HTTP run should not register specialist tools")
	}
	if !shouldRegisterHTTPSpecialistTools(httpapi.RunRequest{Autonomy: "medium"}, "") {
		t.Fatal("medium autonomy HTTP run should register specialist tools")
	}
}

func serveMCPInput(t *testing.T) []byte {
	t.Helper()

	var input bytes.Buffer
	writeServeMCPMessage(t, &input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	})
	writeServeMCPMessage(t, &input, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	writeServeMCPMessage(t, &input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	return input.Bytes()
}

func writeServeMCPMessage(t *testing.T, buffer *bytes.Buffer, message map[string]any) {
	t.Helper()
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(buffer, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}
}
