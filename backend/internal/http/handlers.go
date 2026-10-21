package http

import (
	"context"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
)

// Handlers implements the generated strict server interface.
type Handlers struct{}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// NewHandlers constructs the API handlers.
func NewHandlers() *Handlers {
	return &Handlers{}
}

// GetHealthz is a liveness probe.
func (h *Handlers) GetHealthz(_ context.Context, _ oapi.GetHealthzRequestObject) (oapi.GetHealthzResponseObject, error) {
	return oapi.GetHealthz200JSONResponse{Status: oapi.HealthStatusOk}, nil
}
