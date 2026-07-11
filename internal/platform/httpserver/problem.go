package httpserver

import (
	"encoding/json"
	"net/http"
	"strconv"

	contractapi "github.com/your-org/go-service-template/internal/api"
)

// NewProblem creates an RFC 9457 response body with the current request ID.
func NewProblem(requestID string, status int, title, detail string) contractapi.Problem {
	problemType := "about:blank"
	problem := contractapi.Problem{
		Type:   problemType,
		Title:  title,
		Status: status,
	}
	if detail != "" {
		problem.Detail = &detail
	}
	if requestID != "" {
		problem.RequestId = &requestID
	}
	return problem
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Del("Content-Length")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(NewProblem(RequestID(r.Context()), status, title, detail))
}

func statusTitle(status int) string {
	if title := http.StatusText(status); title != "" {
		return title
	}
	return "HTTP " + strconv.Itoa(status)
}
