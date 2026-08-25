package httpui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"seedvault/internal/domain"
)

func firstIssue(items []domain.Issue) domain.Issue {
	if len(items) == 0 {
		return domain.Issue{}
	}
	return items[0]
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误：" + err.Error()})
		return false
	}
	return true
}

func respond(w http.ResponseWriter, value any, err error, status int) {
	if err != nil {
		fail(w, err)
		return
	}
	write(w, status, value)
}

func fail(w http.ResponseWriter, err error) {
	write(w, errorStatus(err), map[string]string{"error": err.Error()})
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrFrozen):
		return http.StatusLocked
	default:
		return http.StatusUnprocessableEntity
	}
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				write(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func ExpectedVersion(r *http.Request) int {
	version, _ := strconv.Atoi(strings.TrimSpace(r.Header.Get("If-Match")))
	return version
}

func queryInt(value string) int {
	result, _ := strconv.Atoi(value)
	return result
}
