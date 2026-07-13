package usershttp

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractapi "github.com/your-org/go-service-template/internal/api"
	"github.com/your-org/go-service-template/internal/users"
)

func TestHandlerCreateUser(t *testing.T) {
	t.Parallel()

	want := users.User{
		ID:        uuid.MustParse("8d37b313-f867-47bc-8e3d-0953db9c05c8"),
		Email:     "person@example.com",
		CreatedAt: time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC),
	}
	service := &userServiceStub{createUser: want}
	handler := NewHandler(discardLogger(), service, &importServiceStub{})

	response, err := handler.CreateUser(t.Context(), contractapi.CreateUserRequestObject{
		Body: &contractapi.CreateUserJSONRequestBody{Email: openapi_types.Email(want.Email)},
	})
	require.NoError(t, err)

	created, ok := response.(contractapi.CreateUser201JSONResponse)
	require.True(t, ok)
	assert.Equal(t, want.ID, created.Id)
	assert.Equal(t, want.Email, string(created.Email))
	assert.Equal(t, want.CreatedAt, created.CreatedAt)
	assert.Equal(t, want.Email, service.receivedEmail)
}

func TestHandlerMapsUserErrors(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		serviceError   error
		assertResponse func(*testing.T, contractapi.CreateUserResponseObject)
	}{
		"invalid email": {
			serviceError: users.ErrInvalidEmail,
			assertResponse: func(t *testing.T, response contractapi.CreateUserResponseObject) {
				_, ok := response.(contractapi.CreateUser400ApplicationProblemPlusJSONResponse)
				assert.True(t, ok)
			},
		},
		"conflict": {
			serviceError: users.ErrConflict,
			assertResponse: func(t *testing.T, response contractapi.CreateUserResponseObject) {
				_, ok := response.(contractapi.CreateUser409ApplicationProblemPlusJSONResponse)
				assert.True(t, ok)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := NewHandler(discardLogger(), &userServiceStub{createErr: testCase.serviceError}, &importServiceStub{})
			response, err := handler.CreateUser(t.Context(), contractapi.CreateUserRequestObject{
				Body: &contractapi.CreateUserJSONRequestBody{Email: openapi_types.Email("person@example.com")},
			})
			require.NoError(t, err)
			testCase.assertResponse(t, response)
		})
	}
}

func TestHandlerCreatesUserImport(t *testing.T) {
	t.Parallel()

	want := users.Import{
		ID:         uuid.MustParse("0198a1f7-30b7-7df1-8491-c47f6033525b"),
		State:      users.ImportStatePending,
		TotalCount: 2,
		CreatedAt:  time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC),
	}
	imports := &importServiceStub{created: want}
	handler := NewHandler(discardLogger(), &userServiceStub{}, imports)
	response, err := handler.CreateUserImport(t.Context(), contractapi.CreateUserImportRequestObject{
		Body: &contractapi.CreateUserImportJSONRequestBody{Emails: []openapi_types.Email{"one@example.com", "two@example.com"}},
	})
	require.NoError(t, err)

	created, ok := response.(contractapi.CreateUserImport202JSONResponse)
	require.True(t, ok)
	assert.Equal(t, want.ID, created.Id)
	assert.Equal(t, want.TotalCount, created.TotalCount)
	assert.Equal(t, []string{"one@example.com", "two@example.com"}, imports.receivedEmails)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type userServiceStub struct {
	createUser    users.User
	createErr     error
	receivedEmail string
}

func (s *userServiceStub) Create(_ context.Context, email string) (users.User, error) {
	s.receivedEmail = email
	return s.createUser, s.createErr
}

func (s *userServiceStub) Get(context.Context, uuid.UUID) (users.User, error) {
	return users.User{}, nil
}

type importServiceStub struct {
	created        users.Import
	createErr      error
	receivedEmails []string
}

func (s *importServiceStub) Create(_ context.Context, emails []string) (users.Import, error) {
	s.receivedEmails = emails
	return s.created, s.createErr
}

func (s *importServiceStub) Get(context.Context, uuid.UUID) (users.Import, error) {
	return users.Import{}, nil
}
