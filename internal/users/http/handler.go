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

type Handler struct {
	logger *slog.Logger
	users  UserService
}

func NewHandler(logger *slog.Logger, userService UserService) *Handler {
	return &Handler{logger: logger, users: userService}
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
