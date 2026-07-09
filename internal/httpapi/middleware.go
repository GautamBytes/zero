package httpapi

import (
	"net"
	"net/http"
	"strings"
)

func (server *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(server.options.Token)
		if server.options.NoAuth {
			next.ServeHTTP(w, r)
			return
		}
		if token == "" {
			writeError(w, http.StatusInternalServerError, "auth_misconfigured", "HTTP auth token is not configured")
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (server *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && server.corsAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin == "" || server.corsAllowed(origin) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeError(w, http.StatusForbidden, "cors_forbidden", "origin is not allowed")
			return
		}
		if origin != "" && !server.corsAllowed(origin) {
			writeError(w, http.StatusForbidden, "cors_forbidden", "origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (server *Server) corsAllowed(origin string) bool {
	for _, allowed := range server.options.CORS {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func LoopbackHost(hostname string) bool {
	host := strings.TrimSpace(hostname)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
