package service

import (
	"slices"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// AllowedTransitions lists the statuses an order may move to from its current
// status. Courier orders are handed over as "shipped", pickup orders as
// "ready_for_pickup"; "assembling" may be skipped. Cancelling is possible until
// the order is completed (after handover it means the goods came back).
func AllowedTransitions(from db.OrderStatus, method db.DeliveryMethod) []db.OrderStatus {
	handover := db.OrderStatusShipped
	if method == db.DeliveryMethodPickup {
		handover = db.OrderStatusReadyForPickup
	}
	switch from {
	case db.OrderStatusNew:
		return []db.OrderStatus{db.OrderStatusConfirmed, db.OrderStatusCancelled}
	case db.OrderStatusConfirmed:
		return []db.OrderStatus{db.OrderStatusAssembling, handover, db.OrderStatusCancelled}
	case db.OrderStatusAssembling:
		return []db.OrderStatus{handover, db.OrderStatusCancelled}
	case db.OrderStatusShipped, db.OrderStatusReadyForPickup:
		return []db.OrderStatus{db.OrderStatusCompleted, db.OrderStatusCancelled}
	default: // completed, cancelled
		return []db.OrderStatus{}
	}
}

// CanTransition reports whether from → to is allowed.
func CanTransition(from, to db.OrderStatus, method db.DeliveryMethod) bool {
	return slices.Contains(AllowedTransitions(from, method), to)
}
