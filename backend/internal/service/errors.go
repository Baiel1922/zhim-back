// Package service holds the business logic: catalog reads, pricing, promo codes
// and order placement. Transport concerns (JSON shape, localization, image URLs)
// live in internal/http.
package service

import "fmt"

// Code is a stable, client-facing error code (mirrors ErrorCode in openapi.yaml).
type Code string

// Error codes.
const (
	CodeValidation              Code = "validation_error"
	CodeCategoryNotFound        Code = "category_not_found"
	CodeProductNotFound         Code = "product_not_found"
	CodeOrderNotFound           Code = "order_not_found"
	CodeOutOfStock              Code = "out_of_stock"
	CodeDeliveryZoneUnavailable Code = "delivery_zone_unavailable"
	CodePromoNotFound           Code = "promo_not_found"
	CodePromoInactive           Code = "promo_inactive"
	CodePromoNotStarted         Code = "promo_not_started"
	CodePromoExpired            Code = "promo_expired"
	CodePromoExhausted          Code = "promo_exhausted"
	CodePromoMinTotal           Code = "promo_min_total"
	CodeIdempotencyConflict     Code = "idempotency_conflict"
)

// Field error codes.
const (
	FieldRequired = "required"
	FieldInvalid  = "invalid"
	FieldTooLong  = "too_long"
	FieldTooMany  = "too_many"
)

// FieldError points at an invalid request field.
type FieldError struct {
	Field string
	Code  string
}

// StockIssue reports a cart line that cannot be fulfilled.
type StockIssue struct {
	VariantID int64
	Available int32
}

// Error is a business error with a stable code. Anything else returned by the
// service layer is an internal error.
type Error struct {
	Code     Code
	Message  string
	Fields   []FieldError
	Items    []StockIssue
	MinTotal *int32
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func newError(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
