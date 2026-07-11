package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	contract "github.com/your-org/go-service-template/api"
	contractapi "github.com/your-org/go-service-template/internal/api"
)

const maxRequestBodyBytes = 1 << 20

type TokenVerifier interface {
	Verify(context.Context, string) (string, error)
}

type subjectKey struct{}

func Subject(ctx context.Context) (string, bool) {
	subject, ok := ctx.Value(subjectKey{}).(string)
	return subject, ok
}

type Authentication struct {
	disabled bool
	verifier TokenVerifier
}

func DisabledAuthentication() Authentication {
	return Authentication{disabled: true}
}

func TokenAuthentication(verifier TokenVerifier) (Authentication, error) {
	if verifier == nil {
		return Authentication{}, errors.New("token verifier is required")
	}
	return Authentication{verifier: verifier}, nil
}

type Pinger interface {
	Ping(context.Context) error
}

type Readiness struct {
	pinger    Pinger
	accepting atomic.Bool
}

func NewReadiness(pinger Pinger) *Readiness {
	readiness := &Readiness{pinger: pinger}
	readiness.accepting.Store(true)
	return readiness
}

func (r *Readiness) StopAccepting() {
	r.accepting.Store(false)
}

type HandlerOptions struct {
	Logger    *slog.Logger
	API       contractapi.StrictServerInterface
	Auth      Authentication
	Readiness *Readiness
	Version   string
	Commit    string
}

func NewHandler(options HandlerOptions) (http.Handler, error) {
	if options.Logger == nil || options.API == nil || options.Readiness == nil || options.Readiness.pinger == nil {
		return nil, errors.New("logger, API, and readiness are required")
	}
	if !options.Auth.disabled && options.Auth.verifier == nil {
		return nil, errors.New("authentication is not configured")
	}

	spec, err := contractapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI contract: %w", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate OpenAPI contract: %w", err)
	}

	strictHandler := contractapi.NewStrictHandlerWithOptions(options.API, nil, contractapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, r, http.StatusBadRequest, "Bad Request", err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			options.Logger.ErrorContext(r.Context(), "encode response", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "Internal Server Error", "")
		},
	})

	apiMux := http.NewServeMux()
	apiHandler := contractapi.HandlerFromMux(strictHandler, apiMux)
	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: options.Auth.authenticate,
		},
		DoNotValidateServers: true,
		ErrorHandlerWithOpts: validationErrorHandler,
	})
	validatedAPI := requestBodyLimit(validator(apiHandler))

	root := http.NewServeMux()
	root.Handle("/v1/", validatedAPI)
	root.HandleFunc("GET /livez", healthHandler(http.StatusOK, options.Version, options.Commit))
	root.HandleFunc("GET /readyz", readinessHandler(options.Readiness, options.Version, options.Commit))
	root.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(contract.OpenAPI)
	})

	return chain(
		root,
		func(next http.Handler) http.Handler { return requestIDMiddleware(options.Logger, next) },
		func(next http.Handler) http.Handler { return loggingMiddleware(options.Logger, next) },
		func(next http.Handler) http.Handler { return recoveryMiddleware(options.Logger, next) },
	), nil
}

func (a Authentication) authenticate(ctx context.Context, input *openapi3filter.AuthenticationInput) error {
	if input.SecuritySchemeName != "bearerAuth" {
		return fmt.Errorf("unsupported security scheme %q", input.SecuritySchemeName)
	}
	if a.disabled {
		setSubject(input, "development")
		return nil
	}

	headers := input.RequestValidationInput.Request.Header.Values("Authorization")
	if len(headers) != 1 {
		return errors.New("exactly one Authorization header is required")
	}
	token, err := bearerToken(headers[0])
	if err != nil {
		return err
	}
	subject, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return err
	}
	if subject == "" {
		return errors.New("token subject is empty")
	}
	setSubject(input, subject)
	return nil
}

func setSubject(input *openapi3filter.AuthenticationInput, subject string) {
	request := input.RequestValidationInput.Request
	*request = *request.WithContext(context.WithValue(request.Context(), subjectKey{}, subject))
}

func validationErrorHandler(_ context.Context, err error, w http.ResponseWriter, r *http.Request, options nethttpmiddleware.ErrorHandlerOpts) {
	status := options.StatusCode
	if errors.Is(err, routers.ErrMethodNotAllowed) {
		status = http.StatusMethodNotAllowed
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		status = http.StatusRequestEntityTooLarge
	}

	detail := err.Error()
	if status >= http.StatusInternalServerError || status == http.StatusUnauthorized {
		detail = ""
	}
	writeProblem(w, r, status, statusTitle(status), detail)
}

func requestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func healthHandler(status int, version, commit string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, status, version, commit)
	}
}

func readinessHandler(readiness *Readiness, version, commit string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !readiness.accepting.Load() {
			writeHealth(w, http.StatusServiceUnavailable, version, commit)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := readiness.pinger.Ping(ctx); err != nil {
			writeHealth(w, http.StatusServiceUnavailable, version, commit)
			return
		}

		writeHealth(w, http.StatusOK, version, commit)
	}
}

func writeHealth(w http.ResponseWriter, status int, version, commit string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  strings.ToLower(http.StatusText(status)),
		"version": version,
		"commit":  commit,
	})
}
