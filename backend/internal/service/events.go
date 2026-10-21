package service

// OrderEvents learns about order changes after they are committed (the
// Telegram bot). Implementations must return immediately.
type OrderEvents interface {
	OrderCreated(orderID int64)
	OrderUpdated(orderID int64)
}

type noopOrderEvents struct{}

func (noopOrderEvents) OrderCreated(int64) {}
func (noopOrderEvents) OrderUpdated(int64) {}

// SetOrderEvents subscribes e to new orders. Call before serving requests.
func (c *Checkout) SetOrderEvents(e OrderEvents) { c.events = e }

// SetOrderEvents subscribes e to status and payment changes. Call before serving requests.
func (a *Admin) SetOrderEvents(e OrderEvents) { a.events = e }
