package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// errRateLimited is returned by the rate limit middleware.
type errRateLimited struct {
	retryAfter time.Duration
}

func (e errRateLimited) Error() string { return "rate limited" }

func statusFor(code oapi.ErrorCode) int {
	switch code {
	case oapi.ErrorCodeValidationError:
		return http.StatusBadRequest
	case oapi.ErrorCodeNotFound, oapi.ErrorCodeCategoryNotFound, oapi.ErrorCodeProductNotFound, oapi.ErrorCodeOrderNotFound:
		return http.StatusNotFound
	case oapi.ErrorCodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case oapi.ErrorCodeOutOfStock, oapi.ErrorCodeIdempotencyConflict:
		return http.StatusConflict
	case oapi.ErrorCodeRateLimited:
		return http.StatusTooManyRequests
	case oapi.ErrorCodeInternalError:
		return http.StatusInternalServerError
	default: // promo_*, delivery_zone_unavailable
		return http.StatusUnprocessableEntity
	}
}

func writeError(w http.ResponseWriter, body oapi.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusFor(body.Code))
	_ = json.NewEncoder(w).Encode(body)
}

func toAPIError(e *service.Error) oapi.Error {
	body := oapi.Error{Code: oapi.ErrorCode(e.Code), Message: e.Message, MinTotal: e.MinTotal}
	for _, f := range e.Fields {
		body.Fields = append(body.Fields, oapi.FieldError{Field: f.Field, Code: oapi.FieldErrorCode(f.Code)})
	}
	for _, it := range e.Items {
		body.Items = append(body.Items, oapi.StockIssue{VariantID: it.VariantID, Available: it.Available})
	}
	return body
}

func validationError(field, code string) error {
	return &service.Error{
		Code:    service.CodeValidation,
		Message: "invalid " + field,
		Fields:  []service.FieldError{{Field: field, Code: code}},
	}
}

// responseErrorHandler renders errors returned by handlers and strict middleware.
func responseErrorHandler(log *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if e, ok := service.AsError(err); ok {
			writeError(w, toAPIError(e))
			return
		}
		var rl errRateLimited
		if errors.As(err, &rl) {
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.retryAfter.Seconds())+1))
			writeError(w, oapi.Error{Code: oapi.ErrorCodeRateLimited, Message: "too many requests"})
			return
		}
		log.ErrorContext(r.Context(), "request failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.Any("error", err),
		)
		writeError(w, oapi.Error{Code: oapi.ErrorCodeInternalError, Message: "internal error"})
	}
}

// requestErrorHandler renders malformed requests (bad JSON, bad parameters).
func requestErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	body := oapi.Error{Code: oapi.ErrorCodeValidationError, Message: err.Error()}

	var (
		required       *oapi.RequiredParamError
		requiredHeader *oapi.RequiredHeaderError
		invalidFormat  *oapi.InvalidParamFormatError
		tooMany        *oapi.TooManyValuesForParamError
	)
	switch {
	case errors.As(err, &required):
		body.Fields = []oapi.FieldError{{Field: required.ParamName, Code: oapi.FieldErrorCodeRequired}}
	case errors.As(err, &requiredHeader):
		body.Fields = []oapi.FieldError{{Field: requiredHeader.ParamName, Code: oapi.FieldErrorCodeRequired}}
	case errors.As(err, &invalidFormat):
		body.Fields = []oapi.FieldError{{Field: invalidFormat.ParamName, Code: oapi.FieldErrorCodeInvalid}}
	case errors.As(err, &tooMany):
		body.Fields = []oapi.FieldError{{Field: tooMany.ParamName, Code: oapi.FieldErrorCodeInvalid}}
	}
	writeError(w, body)
}
