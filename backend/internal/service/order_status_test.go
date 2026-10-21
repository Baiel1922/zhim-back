package service_test

import (
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

func TestTransitions(t *testing.T) {
	courier, pickup := db.DeliveryMethodCourier, db.DeliveryMethodPickup
	tests := []struct {
		from, to db.OrderStatus
		method   db.DeliveryMethod
		want     bool
	}{
		{db.OrderStatusNew, db.OrderStatusConfirmed, courier, true},
		{db.OrderStatusNew, db.OrderStatusCancelled, courier, true},
		{db.OrderStatusNew, db.OrderStatusShipped, courier, false},
		{db.OrderStatusNew, db.OrderStatusCompleted, courier, false},
		{db.OrderStatusConfirmed, db.OrderStatusAssembling, courier, true},
		{db.OrderStatusConfirmed, db.OrderStatusShipped, courier, true},
		{db.OrderStatusConfirmed, db.OrderStatusReadyForPickup, courier, false},
		{db.OrderStatusConfirmed, db.OrderStatusReadyForPickup, pickup, true},
		{db.OrderStatusConfirmed, db.OrderStatusShipped, pickup, false},
		{db.OrderStatusConfirmed, db.OrderStatusNew, courier, false},
		{db.OrderStatusAssembling, db.OrderStatusShipped, courier, true},
		{db.OrderStatusAssembling, db.OrderStatusReadyForPickup, pickup, true},
		{db.OrderStatusAssembling, db.OrderStatusConfirmed, courier, false},
		{db.OrderStatusShipped, db.OrderStatusCompleted, courier, true},
		{db.OrderStatusShipped, db.OrderStatusCancelled, courier, true},
		{db.OrderStatusReadyForPickup, db.OrderStatusCompleted, pickup, true},
		{db.OrderStatusCompleted, db.OrderStatusCancelled, courier, false},
		{db.OrderStatusCancelled, db.OrderStatusNew, courier, false},
		{db.OrderStatusCancelled, db.OrderStatusConfirmed, pickup, false},
	}
	for _, tt := range tests {
		if got := service.CanTransition(tt.from, tt.to, tt.method); got != tt.want {
			t.Errorf("%s → %s (%s) = %v, want %v", tt.from, tt.to, tt.method, got, tt.want)
		}
	}
}

func TestTerminalStatusesHaveNoTransitions(t *testing.T) {
	for _, s := range []db.OrderStatus{db.OrderStatusCompleted, db.OrderStatusCancelled} {
		if got := service.AllowedTransitions(s, db.DeliveryMethodCourier); len(got) != 0 {
			t.Errorf("%s → %v", s, got)
		}
	}
	// Every status is reachable from "new" with at least one delivery method.
	reachable := map[db.OrderStatus]bool{}
	for _, method := range []db.DeliveryMethod{db.DeliveryMethodCourier, db.DeliveryMethodPickup} {
		seen := map[db.OrderStatus]bool{db.OrderStatusNew: true}
		queue := []db.OrderStatus{db.OrderStatusNew}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			reachable[s] = true
			for _, next := range service.AllowedTransitions(s, method) {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	for _, s := range db.AllOrderStatusValues() {
		if !reachable[s] {
			t.Errorf("status %s is unreachable", s)
		}
	}
}
