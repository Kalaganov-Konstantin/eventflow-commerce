package saga

import "testing"

func TestTransition(t *testing.T) {
	allStates := []State{
		StateStarted, StateStockReserved, StateAwaitingPayment, StatePaid,
		StateCompleted, StateCompensating, StateCompensated, StateFailed,
	}

	validMoves := map[State]map[State]bool{
		StateStarted:         {StateStockReserved: true, StateFailed: true},
		StateStockReserved:   {StateAwaitingPayment: true, StateCompensating: true},
		StateAwaitingPayment: {StatePaid: true, StateCompensating: true},
		StatePaid:            {StateCompleted: true, StateCompensating: true},
		StateCompensating:    {StateCompensated: true},
	}

	for _, from := range allStates {
		for _, to := range allStates {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				err := Transition(from, to)

				if from == to {
					if err != nil {
						t.Errorf("Transition(%q, %q) = %v, want nil (idempotent retry)", from, to, err)
					}
					return
				}

				wantValid := validMoves[from][to]
				if wantValid && err != nil {
					t.Errorf("Transition(%q, %q) = %v, want nil", from, to, err)
				}
				if !wantValid && err == nil {
					t.Errorf("Transition(%q, %q) = nil, want an error", from, to)
				}
			})
		}
	}
}

func TestCanTransition(t *testing.T) {
	if !CanTransition(StateStarted, StateStarted) {
		t.Error("CanTransition(started, started) = false, want true")
	}
	if !CanTransition(StateStarted, StateStockReserved) {
		t.Error("CanTransition(started, stock_reserved) = false, want true")
	}
	if CanTransition(StateCompensated, StateStarted) {
		t.Error("CanTransition(compensated, started) = true, want false")
	}
	if CanTransition(StateFailed, StateStarted) {
		t.Error("CanTransition(failed, started) = true, want false: failed is terminal")
	}
}
