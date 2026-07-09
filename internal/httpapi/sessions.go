package httpapi

import (
	"net/http"
	"strings"

	"github.com/Gitlawb/zero/internal/sessions"
)

func (server *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	list, err := server.store.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (server *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"sessionId"`
		Title     string `json:"title"`
		Cwd       string `json:"cwd"`
		ModelID   string `json:"modelId"`
		Provider  string `json:"provider"`
		Tag       string `json:"tag"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	session, err := server.store.Create(sessions.CreateInput{
		SessionID: strings.TrimSpace(body.SessionID),
		Title:     strings.TrimSpace(body.Title),
		Cwd:       firstNonEmpty(body.Cwd, server.options.Cwd),
		ModelID:   strings.TrimSpace(body.ModelID),
		Provider:  strings.TrimSpace(body.Provider),
		Tag:       strings.TrimSpace(body.Tag),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (server *Server) handleGetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, err := server.store.Get(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if session == nil {
		writeError(w, http.StatusNotFound, "session_not_found", "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (server *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	var body struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	session, err := server.store.UpdateTitle(sessionID, body.Title)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (server *Server) handleSessionEventLog(w http.ResponseWriter, r *http.Request, sessionID string) {
	events, err := server.store.ReadEvents(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (server *Server) handleSessionChildren(w http.ResponseWriter, r *http.Request, sessionID string) {
	children, err := server.store.ListChildren(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"children": children})
}

func (server *Server) handleSessionLineage(w http.ResponseWriter, r *http.Request, sessionID string) {
	lineage, err := server.store.Lineage(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lineage": lineage})
}

func (server *Server) handleSessionTree(w http.ResponseWriter, r *http.Request, sessionID string) {
	tree, err := server.store.Tree(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (server *Server) handleForkSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	var body struct {
		SessionID string `json:"sessionId"`
		Title     string `json:"title"`
		Cwd       string `json:"cwd"`
		ModelID   string `json:"modelId"`
		Provider  string `json:"provider"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	fork, err := server.store.Fork(sessionID, sessions.ForkInput{
		SessionID: strings.TrimSpace(body.SessionID),
		Title:     strings.TrimSpace(body.Title),
		Cwd:       firstNonEmpty(body.Cwd, server.options.Cwd),
		ModelID:   strings.TrimSpace(body.ModelID),
		Provider:  strings.TrimSpace(body.Provider),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, fork)
}
