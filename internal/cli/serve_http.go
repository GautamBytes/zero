package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/httpapi"
	"github.com/Gitlawb/zero/internal/redaction"
	"github.com/Gitlawb/zero/internal/zerogit"
)

func runServeHTTP(options serveOptions, stdout io.Writer, stderr io.Writer, deps appDeps) int {
	_ = stdout
	if options.noAuth && !httpapi.LoopbackHost(options.hostname) {
		return writeExecUsageError(stderr, "--no-auth is only allowed with a loopback --hostname")
	}
	token, generated, err := resolveServeHTTPToken(options)
	if err != nil {
		return writeAppError(stderr, err.Error(), exitCrash)
	}
	store := deps.newSessionStore()
	api := httpapi.New(httpapi.Options{
		Version:        version,
		Cwd:            options.cwd,
		Hostname:       options.hostname,
		Port:           options.port,
		Token:          token,
		NoAuth:         options.noAuth,
		CORS:           options.cors,
		Store:          store,
		Runner:         newHTTPRunner(options.cwd, deps, stderr),
		ConfigSnapshot: serveHTTPConfigSnapshot(options.cwd, deps),
		Provider:       serveHTTPProviderSnapshot(options.cwd, deps),
		Models:         serveHTTPModels(options.cwd, deps),
		VCS: func(ctx context.Context) (any, error) {
			return deps.inspectChanges(ctx, zerogit.InspectOptions{Cwd: options.cwd})
		},
		Now: deps.now,
	})

	addr := net.JoinHostPort(options.hostname, strconv.Itoa(options.port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return writeAppError(stderr, "failed to listen: "+err.Error(), exitCrash)
	}
	server := &http.Server{
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	runCtx, stopSignals := signalContext()
	defer stopSignals()
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	url := "http://" + listener.Addr().String()
	if _, err := fmt.Fprintf(stderr, "[zero] HTTP server listening on %s\n", url); err != nil {
		return exitCrash
	}
	if options.noAuth {
		if _, err := fmt.Fprintln(stderr, "[zero] WARNING: HTTP auth disabled on loopback."); err != nil {
			return exitCrash
		}
	} else {
		if generated {
			if _, err := fmt.Fprintf(stderr, "[zero] HTTP bearer token (generated): %s\n", token); err != nil {
				return exitCrash
			}
		} else {
			if _, err := fmt.Fprintln(stderr, "[zero] HTTP auth enabled with configured bearer token."); err != nil {
				return exitCrash
			}
		}
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return writeAppError(stderr, err.Error(), exitCrash)
	}
	return exitSuccess
}

func resolveServeHTTPToken(options serveOptions) (string, bool, error) {
	if options.noAuth {
		return "", false, nil
	}
	if token := strings.TrimSpace(options.authToken); token != "" {
		return token, false, nil
	}
	if token := strings.TrimSpace(os.Getenv("ZERO_SERVER_TOKEN")); token != "" {
		return token, false, nil
	}
	token, err := randomToken()
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func serveHTTPConfigSnapshot(workspaceRoot string, deps appDeps) func(context.Context) (httpapi.ConfigSnapshot, error) {
	return func(ctx context.Context) (httpapi.ConfigSnapshot, error) {
		_ = ctx
		resolved, err := deps.resolveConfig(workspaceRoot, config.Overrides{})
		if err != nil {
			return httpapi.ConfigSnapshot{}, err
		}
		return httpapi.ConfigSnapshot{
			Version: version,
			Cwd:     workspaceRoot,
			Config: map[string]any{
				"activeProvider": resolved.ActiveProvider,
				"provider":       sanitizedProvider(resolved.Provider),
				"maxTurns":       resolved.MaxTurns,
				"sandbox":        resolved.Sandbox,
				"tools":          resolved.Tools,
				"preferences":    resolved.Preferences,
			},
		}, nil
	}
}

func serveHTTPProviderSnapshot(workspaceRoot string, deps appDeps) func(context.Context) (httpapi.ProviderSnapshot, error) {
	return func(ctx context.Context) (httpapi.ProviderSnapshot, error) {
		_ = ctx
		resolved, err := deps.resolveConfig(workspaceRoot, config.Overrides{})
		if err != nil {
			return httpapi.ProviderSnapshot{}, err
		}
		providers := make([]map[string]any, 0, len(resolved.Providers))
		for _, provider := range resolved.Providers {
			providers = append(providers, sanitizedProvider(provider))
		}
		return httpapi.ProviderSnapshot{
			ActiveProvider: resolved.ActiveProvider,
			Model:          resolved.Provider.Model,
			Providers:      providers,
		}, nil
	}
}

func serveHTTPModels(workspaceRoot string, deps appDeps) func(context.Context) (httpapi.ModelSnapshot, error) {
	return func(ctx context.Context) (httpapi.ModelSnapshot, error) {
		resolved, err := deps.resolveConfig(workspaceRoot, config.Overrides{})
		if err != nil {
			return httpapi.ModelSnapshot{}, err
		}
		models, err := deps.discoverProviderModels(ctx, resolved.Provider)
		if err != nil {
			return httpapi.ModelSnapshot{}, err
		}
		return httpapi.ModelSnapshot{Models: models}, nil
	}
}

func sanitizedProvider(provider config.ProviderProfile) map[string]any {
	result := map[string]any{
		"name":         provider.Name,
		"provider":     provider.Provider,
		"providerKind": provider.ProviderKind,
		"catalogID":    provider.CatalogID,
		"baseURL":      redaction.RedactString(provider.BaseURL, redaction.Options{}),
		"apiFormat":    provider.APIFormat,
		"model":        provider.Model,
		"description":  provider.Description,
	}
	if provider.APIKeyEnv != "" {
		result["apiKeyEnv"] = provider.APIKeyEnv
	}
	if provider.APIKeyStored {
		result["apiKeyStored"] = true
	}
	return result
}
