package httpapi

import (
	"net/http"
)

func (server *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, openAPISpec(server.options.Version))
}

func (server *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>ZERO HTTP API</title></head>
<body>
<h1>ZERO HTTP API</h1>
<p>OpenAPI 3.1 spec: <a href="/openapi.json">/openapi.json</a></p>
</body></html>`))
}

func openAPISpec(version string) map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "ZERO HTTP API",
			"version": firstNonEmpty(version, "dev"),
		},
		"paths": map[string]any{
			"/global/health": method("get", "Health"),
			"/openapi.json":  method("get", "OpenAPI"),
			"/doc":           method("get", "Documentation"),
			"/event":         method("get", "Server-sent events"),
			"/config":        method("get", "Resolved config"),
			"/provider":      method("get", "Provider"),
			"/models":        method("get", "Models"),
			"/path":          method("get", "Path"),
			"/vcs":           method("get", "VCS"),
			"/session": map[string]any{
				"get":  operation("List sessions"),
				"post": operation("Create session"),
			},
			"/session/{id}": map[string]any{
				"get":   operation("Get session"),
				"patch": operation("Update session"),
			},
			"/session/{id}/event-log":               method("get", "Session event log"),
			"/session/{id}/children":                method("get", "Session children"),
			"/session/{id}/lineage":                 method("get", "Session lineage"),
			"/session/{id}/tree":                    method("get", "Session tree"),
			"/session/{id}/fork":                    method("post", "Fork session"),
			"/session/{id}/abort":                   method("post", "Abort active run"),
			"/session/{id}/message":                 method("post", "Run prompt synchronously"),
			"/session/{id}/prompt_async":            method("post", "Run prompt asynchronously"),
			"/session/{id}/permissions/{requestId}": method("post", "Resolve permission request"),
			"/session/{id}/ask/{requestId}":         method("post", "Answer ask_user request"),
			"/file":                                 method("get", "File metadata"),
			"/file/content":                         method("get", "File content"),
			"/file/status":                          method("get", "File status"),
			"/find":                                 method("get", "Search file contents"),
			"/find/file":                            method("get", "Search file paths"),
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "opaque",
				},
			},
			"schemas": map[string]any{
				"Error": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"error": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"code":    map[string]any{"type": "string"},
								"message": map[string]any{"type": "string"},
							},
							"required": []string{"code", "message"},
						},
					},
					"required": []string{"error"},
				},
				"PromptRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content":         map[string]any{"type": "string"},
						"model":           map[string]any{"type": "string"},
						"reasoningEffort": map[string]any{"type": "string"},
						"permissionMode":  map[string]any{"type": "string"},
						"autonomy":        map[string]any{"type": "string"},
						"images":          map[string]any{"type": "array"},
					},
				},
			},
		},
		"security": []map[string][]string{{"bearerAuth": []string{}}},
	}
}

func method(method string, summary string) map[string]any {
	return map[string]any{method: operation(summary)}
}

func operation(summary string) map[string]any {
	return map[string]any{
		"summary": summary,
		"responses": map[string]any{
			"200": map[string]any{"description": "OK"},
			"400": map[string]any{"description": "Bad request"},
			"401": map[string]any{"description": "Unauthorized"},
			"404": map[string]any{"description": "Not found"},
			"409": map[string]any{"description": "Conflict"},
			"500": map[string]any{"description": "Internal error"},
		},
	}
}
