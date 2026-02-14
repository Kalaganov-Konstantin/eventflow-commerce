// Package saga models the order saga's compensation-aware lifecycle: reserving stock, waiting for
// payment, and undoing earlier steps when a later one fails.
package saga

import "fmt"

// State is a step of the order saga.
type State string

const (
	StateStarted         State = "started"
	StateStockReserved   State = "stock_reserved"
	StateAwaitingPayment State = "awaiting_payment"
	StatePaid            State = "paid"
	StateCompleted       State = "completed"
	StateCompensating    State = "compensating"
	StateCompensated     State = "compensated"
	StateFailed          State = "failed"
)

// graph enumerates the valid moves out of each state, both forward and compensating. A state
// absent from the map has no outgoing moves.
var graph = map[State][]State{
	StateStarted:         {StateStockReserved, StateFailed},
	StateStockReserved:   {StateAwaitingPayment, StateCompensating},
	StateAwaitingPayment: {StatePaid, StateCompensating},
	StatePaid:            {StateCompleted, StateCompensating},
	StateCompensating:    {StateCompensated},
}

// CanTransition reports whether moving from `from` to `to` is part of the saga's state graph. A
// transition to the state a saga is already in is always allowed, so retrying a step that already
// landed is a no-op rather than an error.
func CanTransition(from, to State) bool {
	if from == to {
		return true
	}
	for _, allowed := range graph[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Transition validates moving from `from` to `to`, returning an error describing the rejected move
// when it is not part of the saga's state graph.
func Transition(from, to State) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid saga transition from %q to %q", from, to)
	}
	return nil
}
