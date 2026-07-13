package httpserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contract "github.com/your-org/go-service-template/api"
	contractapi "github.com/your-org/go-service-template/internal/api"
	"github.com/your-org/go-service-template/internal/platform/httpserver"
)

func TestHandlerValidatesRequest(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &apiStub{}, httpserver.DisabledAuthentication(), pingerStub{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/users", strings.NewReader(`{"email":"person@example.com","admin":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	assert.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
}

func TestHandlerRequiresBearerToken(t *testing.T) {
	t.Parallel()

	authentication, err := httpserver.TokenAuthentication(&tokenVerifierStub{})
	require.NoError(t, err)
	handler := newTestHandler(t, &apiStub{}, authentication, pingerStub{})

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/users/8d37b313-f867-47bc-8e3d-0953db9c05c8", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
}

func TestHandlerVerifiesBearerToken(t *testing.T) {
	t.Parallel()

	verifier := &tokenVerifierStub{}
	authentication, err := httpserver.TokenAuthentication(verifier)
	require.NoError(t, err)
	api := &apiStub{}
	handler := newTestHandler(t, api, authentication, pingerStub{})

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/users/8d37b313-f867-47bc-8e3d-0953db9c05c8", nil)
	request.Header.Set("Authorization", "Bearer signed.jwt.token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "signed.jwt.token", verifier.token)
	assert.Equal(t, "user-123", api.receivedSubject)
}

func TestHandlerProbes(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &apiStub{}, httpserver.DisabledAuthentication(), pingerStub{err: errors.New("database unavailable")})

	for path, wantStatus := range map[string]int{
		"/livez":  http.StatusOK,
		"/readyz": http.StatusServiceUnavailable,
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assert.Equal(t, wantStatus, response.Code, "GET %s", path)
	}
}

func TestHandlerServesCanonicalContract(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &apiStub{}, httpserver.DisabledAuthentication(), pingerStub{})
	for path, want := range map[string][]byte{
		"/openapi.yaml":  contract.OpenAPI,
		"/asyncapi.yaml": contract.AsyncAPI,
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code, "GET %s", path)
		assert.Equal(t, "application/yaml", response.Header().Get("Content-Type"), "GET %s", path)
		assert.Equal(t, string(want), response.Body.String(), "GET %s", path)
	}
}

func TestHandlerServesMetrics(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &apiStub{}, httpserver.DisabledAuthentication(), pingerStub{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "service_requests_total 1\n", response.Body.String())
}

func TestHandlerRequestID(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &apiStub{}, httpserver.DisabledAuthentication(), pingerStub{})

	t.Run("generates UUIDv7", func(t *testing.T) {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/livez", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		requestID, err := uuid.Parse(response.Header().Get("X-Request-ID"))
		require.NoError(t, err)
		assert.Equal(t, uuid.Version(7), requestID.Version())
	})

	t.Run("preserves an incoming value", func(t *testing.T) {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/livez", nil)
		request.Header.Set("X-Request-ID", "correlation-123")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assert.Equal(t, "correlation-123", response.Header().Get("X-Request-ID"))
	})
}

func newTestHandler(t *testing.T, api contractapi.StrictServerInterface, authentication httpserver.Authentication, pinger httpserver.Pinger) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := httpserver.NewHandler(httpserver.HandlerOptions{
		Logger:    logger,
		API:       api,
		Auth:      authentication,
		Readiness: httpserver.NewReadiness(pinger, nil),
		Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "service_requests_total 1\n")
		}),
		Version: "test",
		Commit:  "test",
	})
	require.NoError(t, err)
	return handler
}

type apiStub struct {
	receivedSubject string
}

func (s *apiStub) CreateUserImport(context.Context, contractapi.CreateUserImportRequestObject) (contractapi.CreateUserImportResponseObject, error) {
	return contractapi.CreateUserImport202JSONResponse{}, nil
}

func (s *apiStub) GetUserImport(context.Context, contractapi.GetUserImportRequestObject) (contractapi.GetUserImportResponseObject, error) {
	return contractapi.GetUserImport200JSONResponse{}, nil
}

func (s *apiStub) CreateUser(_ context.Context, request contractapi.CreateUserRequestObject) (contractapi.CreateUserResponseObject, error) {
	return contractapi.CreateUser201JSONResponse{
		Id:        uuid.MustParse("8d37b313-f867-47bc-8e3d-0953db9c05c8"),
		Email:     request.Body.Email,
		CreatedAt: time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (s *apiStub) GetUser(ctx context.Context, request contractapi.GetUserRequestObject) (contractapi.GetUserResponseObject, error) {
	s.receivedSubject, _ = httpserver.Subject(ctx)
	return contractapi.GetUser200JSONResponse{
		Id:        request.UserId,
		Email:     openapi_types.Email("person@example.com"),
		CreatedAt: time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC),
	}, nil
}

type pingerStub struct {
	err error
}

func (p pingerStub) Ping(context.Context) error {
	return p.err
}

type tokenVerifierStub struct {
	token string
}

func (v *tokenVerifierStub) Verify(_ context.Context, token string) (string, error) {
	v.token = token
	return "user-123", nil
}
