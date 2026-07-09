package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

type domainError struct {
	status  int
	code    string
	message string
}

func (err domainError) Error() string {
	return err.message
}

func notFoundError(code string, message string) error {
	return domainError{status: http.StatusNotFound, code: code, message: message}
}

func writeDomainError(w http.ResponseWriter, err error) {
	var domain domainError
	if errors.As(err, &domain) {
		writeError(w, domain.status, domain.code, domain.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var domain domainError
	if errors.As(err, &domain) {
		writeError(w, domain.status, domain.code, domain.message)
		return
	}
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
}

func writeStoreError(w http.ResponseWriter, err error) {
	message := err.Error()
	if strings.Contains(message, "not found") {
		writeError(w, http.StatusNotFound, "session_not_found", message)
		return
	}
	if strings.Contains(message, "invalid zero session id") {
		writeError(w, http.StatusBadRequest, "invalid_session_id", message)
		return
	}
	writeError(w, http.StatusInternalServerError, "session_error", message)
}
