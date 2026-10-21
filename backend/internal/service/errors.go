// Package service holds the business logic: catalog reads, pricing, promo codes
// and order placement. Transport concerns (JSON shape, localization, image URLs)
// live in internal/http.
package service

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

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

	CodeNotFound                Code = "not_found"
	CodeUnauthorized            Code = "unauthorized"
	CodeForbidden               Code = "forbidden"
	CodeInitDataInvalid         Code = "init_data_invalid"
	CodeInitDataExpired         Code = "init_data_expired"
	CodeNotAdmin                Code = "not_admin"
	CodeAlreadyExists           Code = "already_exists"
	CodeInUse                   Code = "in_use"
	CodeInvalidStatusTransition Code = "invalid_status_transition"
	CodeLastOwner               Code = "last_owner"
	CodeFileTooLarge            Code = "file_too_large"
	CodeUnsupportedMediaType    Code = "unsupported_media_type"
)

// Field error codes.
const (
	FieldRequired = "required"
	FieldInvalid  = "invalid"
	FieldTooLong  = "too_long"
	FieldTooMany  = "too_many"
	FieldTaken    = "taken"
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

// fields collects validation errors while normalizing input.
type fields []FieldError

func (f *fields) add(field, code string) { *f = append(*f, FieldError{field, code}) }

// text trims v and checks it is non-empty and at most maxLen runes.
func (f *fields) text(field, v string, maxLen int) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		f.add(field, FieldRequired)
	case utf8.RuneCountInString(v) > maxLen:
		f.add(field, FieldTooLong)
	}
	return v
}

// optionalText trims v and checks it is at most maxLen runes.
func (f *fields) optionalText(field, v string, maxLen int) string {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > maxLen {
		f.add(field, FieldTooLong)
	}
	return v
}

// err returns a validation error, or nil if nothing was collected.
func (f fields) err() error {
	if len(f) == 0 {
		return nil
	}
	return &Error{Code: CodeValidation, Message: "invalid input", Fields: f}
}

func notFound(what string) *Error {
	return newError(CodeNotFound, "%s not found", what)
}

func taken(field string) *Error {
	return &Error{Code: CodeAlreadyExists, Message: field + " is already taken", Fields: []FieldError{{field, FieldTaken}}}
}

func invalidField(field string) *Error {
	return &Error{Code: CodeValidation, Message: "invalid " + field, Fields: []FieldError{{field, FieldInvalid}}}
}
