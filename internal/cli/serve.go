package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Gitlawb/zero/internal/mcp"
	"github.com/Gitlawb/zero/internal/redaction"
	"github.com/Gitlawb/zero/internal/tools"
)

type serveOptions struct {
	mcp              bool
	http             bool
	cwd              string
	hostname         string
	hostnameSet      bool
	port             int
	portSet          bool
	cors             []string
	authToken        string
	noAuth           bool
	allowUnsafeTools bool
}

func runServe(args []string, stdout io.Writer, stderr io.Writer, deps appDeps) int {
	options, help, err := parseServeArgs(args)
	if err != nil {
		return writeExecUsageError(stderr, err.Error())
	}
	if help {
		if err := writeServeHelp(stdout); err != nil {
			return exitCrash
		}
		return exitSuccess
	}
	if options.mcp == options.http {
		return writeExecUsageError(stderr, "serve requires exactly one of --mcp or --http")
	}
	if options.http && options.allowUnsafeTools {
		return writeExecUsageError(stderr, "--allow-unsafe-tools is only supported with --mcp")
	}
	if options.noAuth && strings.TrimSpace(options.authToken) != "" {
		return writeExecUsageError(stderr, "use either --no-auth or --auth-token, not both")
	}
	if options.mcp && (options.noAuth || strings.TrimSpace(options.authToken) != "" || len(options.cors) > 0 || options.hostnameSet || options.portSet) {
		return writeExecUsageError(stderr, "HTTP flags require --http")
	}

	workspaceRoot, err := resolveWorkspaceRoot(options.cwd, deps)
	if err != nil {
		return writeExecUsageError(stderr, err.Error())
	}
	if options.http {
		options.cwd = workspaceRoot
		return runServeHTTP(options, stdout, stderr, deps)
	}
	registry := newServeRegistry(workspaceRoot, options.allowUnsafeTools)
	if options.allowUnsafeTools {
		if _, err := fmt.Fprintln(stderr, "[zero] Unsafe MCP server tools enabled because --allow-unsafe-tools was passed."); err != nil {
			return exitCrash
		}
	}

	err = mcp.Serve(context.Background(), deps.stdin, stdout, registry, mcp.ServeOptions{
		Name:              "zero",
		Version:           version,
		PermissionGranted: options.allowUnsafeTools,
	})
	if err != nil {
		return writeAppError(stderr, redaction.ErrorMessage(err, redaction.Options{}), exitCrash)
	}
	return exitSuccess
}

func newServeRegistry(workspaceRoot string, allowUnsafeTools bool) *tools.Registry {
	registry := tools.NewRegistry()
	toolset := tools.CoreReadOnlyTools(workspaceRoot)
	if allowUnsafeTools {
		toolset = tools.CoreTools(workspaceRoot)
	}
	for _, tool := range toolset {
		registry.Register(tool)
	}
	return registry
}

func parseServeArgs(args []string) (serveOptions, bool, error) {
	options := serveOptions{hostname: "127.0.0.1", port: 4096}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help" || arg == "help":
			return options, true, nil
		case arg == "--mcp":
			options.mcp = true
		case arg == "--http":
			options.http = true
		case arg == "--allow-unsafe-tools":
			options.allowUnsafeTools = true
		case arg == "--hostname":
			index++
			if index >= len(args) {
				return options, false, execUsageError{"--hostname requires a value"}
			}
			options.hostname = args[index]
			options.hostnameSet = true
		case strings.HasPrefix(arg, "--hostname="):
			options.hostname = strings.TrimPrefix(arg, "--hostname=")
			options.hostnameSet = true
		case arg == "--port":
			index++
			if index >= len(args) {
				return options, false, execUsageError{"--port requires a value"}
			}
			port, err := strconv.Atoi(args[index])
			if err != nil || port <= 0 || port > 65535 {
				return options, false, execUsageError{"--port must be a TCP port from 1 to 65535"}
			}
			options.port = port
			options.portSet = true
		case strings.HasPrefix(arg, "--port="):
			port, err := strconv.Atoi(strings.TrimPrefix(arg, "--port="))
			if err != nil || port <= 0 || port > 65535 {
				return options, false, execUsageError{"--port must be a TCP port from 1 to 65535"}
			}
			options.port = port
			options.portSet = true
		case arg == "--cors":
			index++
			if index >= len(args) {
				return options, false, execUsageError{"--cors requires an origin"}
			}
			options.cors = append(options.cors, args[index])
		case strings.HasPrefix(arg, "--cors="):
			options.cors = append(options.cors, strings.TrimPrefix(arg, "--cors="))
		case arg == "--auth-token":
			index++
			if index >= len(args) {
				return options, false, execUsageError{"--auth-token requires a token"}
			}
			options.authToken = args[index]
		case strings.HasPrefix(arg, "--auth-token="):
			options.authToken = strings.TrimPrefix(arg, "--auth-token=")
		case arg == "--no-auth":
			options.noAuth = true
		case arg == "-C" || arg == "--cwd":
			index++
			if index >= len(args) {
				return options, false, execUsageError{arg + " requires a path"}
			}
			options.cwd = args[index]
		case strings.HasPrefix(arg, "--cwd="):
			options.cwd = strings.TrimPrefix(arg, "--cwd=")
		default:
			return options, false, execUsageError{fmt.Sprintf("unknown serve flag %q", arg)}
		}
	}
	return options, false, nil
}

func writeServeHelp(w io.Writer) error {
	_, err := fmt.Fprint(w, `Usage:
  zero serve --mcp [flags]
  zero serve --http [flags]

Starts Zero as an MCP stdio server or local HTTP automation server.

Flags:
      --mcp                   Run the MCP stdio server
      --http                  Run the HTTP/OpenAPI server
      --hostname <host>       HTTP bind host (default 127.0.0.1)
      --port <port>           HTTP bind port (default 4096)
      --cors <origin>         Allow a browser origin (repeatable)
      --auth-token <token>    Require this bearer token
      --no-auth               Disable auth on loopback only
  -C, --cwd <path>            Set the workspace directory
      --allow-unsafe-tools    Expose write and shell tools to the MCP host
  -h, --help                  Show this help
`)
	return err
}
