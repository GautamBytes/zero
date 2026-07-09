package httpapi

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func (server *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	target, err := server.safePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file_not_found", "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "file_error", err.Error())
		return
	}
	payload := fileInfoPayload(server.options.Cwd, target, info)
	if info.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "file_error", err.Error())
			return
		}
		children := make([]any, 0, len(entries))
		for _, entry := range entries {
			childInfo, childErr := entry.Info()
			if childErr != nil {
				continue
			}
			children = append(children, fileInfoPayload(server.options.Cwd, filepath.Join(target, entry.Name()), childInfo))
		}
		payload["children"] = children
	}
	writeJSON(w, http.StatusOK, payload)
}

func (server *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	target, err := server.safePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file_not_found", "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "file_error", err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "not_file", "path is a directory")
		return
	}
	if info.Size() > server.options.MaxFileBytes {
		writeError(w, http.StatusBadRequest, "file_too_large", "file is too large")
		return
	}
	data, err := os.ReadFile(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "file_error", err.Error())
		return
	}
	rel, _ := filepath.Rel(server.options.Cwd, target)
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    filepath.ToSlash(rel),
		"content": string(data),
	})
}

func (server *Server) handleFileStatus(w http.ResponseWriter, r *http.Request) {
	server.handleVCS(w, r)
}

func (server *Server) handleFind(w http.ResponseWriter, r *http.Request) {
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		writeError(w, http.StatusBadRequest, "pattern_required", "pattern is required")
		return
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pattern", err.Error())
		return
	}
	matches := []map[string]any{}
	err = filepath.WalkDir(server.options.Cwd, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || len(matches) >= 200 {
			return nil
		}
		if shouldSkipPath(server.options.Cwd, path, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > server.options.MaxFileBytes {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		rel, _ := filepath.Rel(server.options.Cwd, path)
		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, map[string]any{
					"path": filepath.ToSlash(rel),
					"line": lineNo,
					"text": line,
				})
				if len(matches) >= 200 {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (server *Server) handleFindFile(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query_required", "query is required")
		return
	}
	matches := []string{}
	err := filepath.WalkDir(server.options.Cwd, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || len(matches) >= 200 {
			return nil
		}
		if shouldSkipPath(server.options.Cwd, path, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, query) {
			rel, _ := filepath.Rel(server.options.Cwd, path)
			matches = append(matches, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_error", err.Error())
		return
	}
	sort.Strings(matches)
	writeJSON(w, http.StatusOK, map[string]any{"files": matches})
}

func (server *Server) safePath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = "."
	}
	base, err := filepath.Abs(server.options.Cwd)
	if err != nil {
		return "", err
	}
	target := value
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", domainError{status: http.StatusBadRequest, code: "path_outside_workspace", message: "path is outside the workspace"}
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	if resolvedTarget, err := filepath.EvalSymlinks(target); err == nil {
		rel, err := filepath.Rel(resolvedBase, resolvedTarget)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", domainError{status: http.StatusBadRequest, code: "path_outside_workspace", message: "path is outside the workspace"}
		}
		return resolvedTarget, nil
	}
	return target, nil
}

func fileInfoPayload(base string, path string, info os.FileInfo) map[string]any {
	rel, _ := filepath.Rel(base, path)
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return map[string]any{
		"path":    filepath.ToSlash(rel),
		"type":    kind,
		"size":    info.Size(),
		"modTime": info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func shouldSkipPath(base string, path string, entry os.DirEntry) bool {
	name := entry.Name()
	if name == ".git" || name == "node_modules" || name == ".zero" {
		return true
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return true
	}
	if path == base {
		return false
	}
	if strings.HasPrefix(name, ".") && entry.IsDir() {
		return true
	}
	return false
}
