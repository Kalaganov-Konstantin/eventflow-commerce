package errors

import (
	"net/http"
	"testing"
)

func TestConstructors(t *testing.T) {
	testCases := []struct {
		name         string
		err          *AppError
		wantCode     string
		wantHTTPCode int
		wantMessage  string
		wantDetails  string
	}{
		{
			name:         "New",
			err:          New("SOME_CODE", "some message"),
			wantCode:     "SOME_CODE",
			wantHTTPCode: http.StatusInternalServerError,
			wantMessage:  "some message",
		},
		{
			name:         "NewWithDetails",
			err:          NewWithDetails("SOME_CODE", "some message", "some details"),
			wantCode:     "SOME_CODE",
			wantHTTPCode: http.StatusInternalServerError,
			wantMessage:  "some message",
			wantDetails:  "some details",
		},
		{
			name:         "NewBadRequest",
			err:          NewBadRequest("invalid input"),
			wantCode:     "BAD_REQUEST",
			wantHTTPCode: http.StatusBadRequest,
			wantMessage:  "invalid input",
		},
		{
			name:         "NewNotFound",
			err:          NewNotFound("order"),
			wantCode:     "NOT_FOUND",
			wantHTTPCode: http.StatusNotFound,
			wantMessage:  "order not found",
		},
		{
			name:         "NewUnauthorized",
			err:          NewUnauthorized("missing token"),
			wantCode:     "UNAUTHORIZED",
			wantHTTPCode: http.StatusUnauthorized,
			wantMessage:  "missing token",
		},
		{
			name:         "NewForbidden",
			err:          NewForbidden("no access"),
			wantCode:     "FORBIDDEN",
			wantHTTPCode: http.StatusForbidden,
			wantMessage:  "no access",
		},
		{
			name:         "NewConflict",
			err:          NewConflict("already exists"),
			wantCode:     "CONFLICT",
			wantHTTPCode: http.StatusConflict,
			wantMessage:  "already exists",
		},
		{
			name:         "NewInternalServerError",
			err:          NewInternalServerError("boom"),
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantHTTPCode: http.StatusInternalServerError,
			wantMessage:  "boom",
		},
		{
			name:         "NewValidationError",
			err:          NewValidationError("email", "must not be empty"),
			wantCode:     "VALIDATION_ERROR",
			wantHTTPCode: http.StatusBadRequest,
			wantMessage:  "Validation failed for field 'email': must not be empty",
		},
		{
			name:         "NewOrderNotFound",
			err:          NewOrderNotFound("order-1"),
			wantCode:     "ORDER_NOT_FOUND",
			wantHTTPCode: http.StatusNotFound,
			wantMessage:  "Order not found",
			wantDetails:  "Order with ID order-1 does not exist",
		},
		{
			name:         "NewInsufficientInventory",
			err:          NewInsufficientInventory("product-1", 5, 2),
			wantCode:     "INSUFFICIENT_INVENTORY",
			wantHTTPCode: http.StatusConflict,
			wantMessage:  "Insufficient inventory",
			wantDetails:  "Product product-1: requested 5, available 2",
		},
		{
			name:         "NewPaymentFailed",
			err:          NewPaymentFailed("card declined"),
			wantCode:     "PAYMENT_FAILED",
			wantHTTPCode: http.StatusPaymentRequired,
			wantMessage:  "Payment processing failed",
			wantDetails:  "card declined",
		},
		{
			name:         "NewProductNotFound",
			err:          NewProductNotFound("product-1"),
			wantCode:     "PRODUCT_NOT_FOUND",
			wantHTTPCode: http.StatusNotFound,
			wantMessage:  "Product not found",
			wantDetails:  "Product with ID product-1 does not exist",
		},
		{
			name:         "NewOrderAlreadyProcessed",
			err:          NewOrderAlreadyProcessed("order-1"),
			wantCode:     "ORDER_ALREADY_PROCESSED",
			wantHTTPCode: http.StatusConflict,
			wantMessage:  "Order has already been processed",
			wantDetails:  "Order order-1 cannot be modified in its current state",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", tc.err.Code, tc.wantCode)
			}
			if tc.err.HTTPCode != tc.wantHTTPCode {
				t.Errorf("HTTPCode = %d, want %d", tc.err.HTTPCode, tc.wantHTTPCode)
			}
			if tc.err.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", tc.err.Message, tc.wantMessage)
			}
			if tc.err.Details != tc.wantDetails {
				t.Errorf("Details = %q, want %q", tc.err.Details, tc.wantDetails)
			}

			wantErrorString := "[" + tc.wantCode + "] " + tc.wantMessage
			if tc.wantDetails != "" {
				wantErrorString += ": " + tc.wantDetails
			}
			if got := tc.err.Error(); got != wantErrorString {
				t.Errorf("Error() = %q, want %q", got, wantErrorString)
			}
		})
	}
}
