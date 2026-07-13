package usershttp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	contractapi "github.com/your-org/go-service-template/internal/api"
	"github.com/your-org/go-service-template/internal/platform/httpserver"
	"github.com/your-org/go-service-template/internal/users"
)

type UserService interface {
	Create(context.Context, string) (users.User, error)
	Get(context.Context, uuid.UUID) (users.User, error)
}

type ImportService interface {
	Create(context.Context, []string) (users.Import, error)
	Get(context.Context, uuid.UUID) (users.Import, error)
}

type Handler struct {
	logger  *slog.Logger
	users   UserService
	imports ImportService
}

func NewHandler(logger *slog.Logger, userService UserService, importService ImportService) *Handler {
	return &Handler{logger: logger, users: userService, imports: importService}
}

func (h *Handler) CreateUserImport(ctx context.Context, request contractapi.CreateUserImportRequestObject) (contractapi.CreateUserImportResponseObject, error) {
	emails := make([]string, len(request.Body.Emails))
	for index, email := range request.Body.Emails {
		emails[index] = string(email)
	}

	userImport, err := h.imports.Create(ctx, emails)
	if err != nil {
		if errors.Is(err, users.ErrInvalidImport) {
			return contractapi.CreateUserImport400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: contractapi.BadRequestApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 400, "Bad Request", "emails must contain 1 to 100 unique valid addresses"),
				),
			}, nil
		}
		h.logUnexpected(ctx, err)
		return contractapi.CreateUserImport500ApplicationProblemPlusJSONResponse{
			InternalErrorApplicationProblemPlusJSONResponse: contractapi.InternalErrorApplicationProblemPlusJSONResponse(
				httpserver.NewProblem(httpserver.RequestID(ctx), 500, "Internal Server Error", ""),
			),
		}, nil
	}

	return contractapi.CreateUserImport202JSONResponse(toAPIImport(userImport)), nil
}

func (h *Handler) GetUserImport(ctx context.Context, request contractapi.GetUserImportRequestObject) (contractapi.GetUserImportResponseObject, error) {
	userImport, err := h.imports.Get(ctx, request.ImportId)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return contractapi.GetUserImport404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: contractapi.NotFoundApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 404, "Not Found", "user import not found"),
				),
			}, nil
		}
		h.logUnexpected(ctx, err)
		return contractapi.GetUserImport500ApplicationProblemPlusJSONResponse{
			InternalErrorApplicationProblemPlusJSONResponse: contractapi.InternalErrorApplicationProblemPlusJSONResponse(
				httpserver.NewProblem(httpserver.RequestID(ctx), 500, "Internal Server Error", ""),
			),
		}, nil
	}

	return contractapi.GetUserImport200JSONResponse(toAPIImport(userImport)), nil
}

func (h *Handler) CreateUser(ctx context.Context, request contractapi.CreateUserRequestObject) (contractapi.CreateUserResponseObject, error) {
	user, err := h.users.Create(ctx, string(request.Body.Email))
	if err != nil {
		switch {
		case errors.Is(err, users.ErrInvalidEmail):
			return contractapi.CreateUser400ApplicationProblemPlusJSONResponse{
				BadRequestApplicationProblemPlusJSONResponse: contractapi.BadRequestApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 400, "Bad Request", "invalid email"),
				),
			}, nil
		case errors.Is(err, users.ErrConflict):
			return contractapi.CreateUser409ApplicationProblemPlusJSONResponse{
				ConflictApplicationProblemPlusJSONResponse: contractapi.ConflictApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 409, "Conflict", "a user with this email already exists"),
				),
			}, nil
		default:
			h.logUnexpected(ctx, err)
			return contractapi.CreateUser500ApplicationProblemPlusJSONResponse{
				InternalErrorApplicationProblemPlusJSONResponse: contractapi.InternalErrorApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 500, "Internal Server Error", ""),
				),
			}, nil
		}
	}

	return contractapi.CreateUser201JSONResponse(toAPIUser(user)), nil
}

func (h *Handler) GetUser(ctx context.Context, request contractapi.GetUserRequestObject) (contractapi.GetUserResponseObject, error) {
	user, err := h.users.Get(ctx, request.UserId)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return contractapi.GetUser404ApplicationProblemPlusJSONResponse{
				NotFoundApplicationProblemPlusJSONResponse: contractapi.NotFoundApplicationProblemPlusJSONResponse(
					httpserver.NewProblem(httpserver.RequestID(ctx), 404, "Not Found", "user not found"),
				),
			}, nil
		}

		h.logUnexpected(ctx, err)
		return contractapi.GetUser500ApplicationProblemPlusJSONResponse{
			InternalErrorApplicationProblemPlusJSONResponse: contractapi.InternalErrorApplicationProblemPlusJSONResponse(
				httpserver.NewProblem(httpserver.RequestID(ctx), 500, "Internal Server Error", ""),
			),
		}, nil
	}

	return contractapi.GetUser200JSONResponse(toAPIUser(user)), nil
}

func (h *Handler) logUnexpected(ctx context.Context, err error) {
	h.logger.ErrorContext(ctx, "request failed",
		"error", err,
		"request_id", httpserver.RequestID(ctx),
	)
}

func toAPIUser(user users.User) contractapi.User {
	return contractapi.User{
		Id:        user.ID,
		Email:     openapi_types.Email(user.Email),
		CreatedAt: user.CreatedAt,
	}
}

func toAPIImport(userImport users.Import) contractapi.UserImport {
	return contractapi.UserImport{
		Id:             userImport.ID,
		State:          contractapi.UserImportState(userImport.State),
		TotalCount:     userImport.TotalCount,
		CompletedCount: userImport.CompletedCount,
		FailedCount:    userImport.FailedCount,
		CreatedAt:      userImport.CreatedAt,
		StartedAt:      userImport.StartedAt,
		FinishedAt:     userImport.FinishedAt,
	}
}
