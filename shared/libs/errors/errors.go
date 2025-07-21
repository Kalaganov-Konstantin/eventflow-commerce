package errors

import (
	"fmt"
	"net/http"
)

type AppError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Details  string `json:"details,omitempty"`
	HTTPCode int    `json:"-"`
}

func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func New(code, message string) *AppError {
	return &AppError{
		Code:     code,
		Message:  message,
		HTTPCode: http.StatusInternalServerError,
	}
}

func NewWithDetails(code, message, details string) *AppError {
	return &AppError{
		Code:     code,
		Message:  message,
		Details:  details,
		HTTPCode: http.StatusInternalServerError,
	}
}

func NewBadRequest(message string) *AppError {
	return &AppError{
		Code:     "BAD_REQUEST",
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

func NewNotFound(resource string) *AppError {
	return &AppError{
		Code:     "NOT_FOUND",
		Message:  fmt.Sprintf("%s not found", resource),
		HTTPCode: http.StatusNotFound,
	}
}

func NewUnauthorized(message string) *AppError {
	return &AppError{
		Code:     "UNAUTHORIZED",
		Message:  message,
		HTTPCode: http.StatusUnauthorized,
	}
}

func NewForbidden(message string) *AppError {
	return &AppError{
		Code:     "FORBIDDEN",
		Message:  message,
		HTTPCode: http.StatusForbidden,
	}
}

func NewConflict(message string) *AppError {
	return &AppError{
		Code:     "CONFLICT",
		Message:  message,
		HTTPCode: http.StatusConflict,
	}
}

func NewInternalServerError(message string) *AppError {
	return &AppError{
		Code:     "INTERNAL_SERVER_ERROR",
		Message:  message,
		HTTPCode: http.StatusInternalServerError,
	}
}

func NewValidationError(field, message string) *AppError {
	return &AppError{
		Code:     "VALIDATION_ERROR",
		Message:  fmt.Sprintf("Validation failed for field '%s': %s", field, message),
		HTTPCode: http.StatusBadRequest,
	}
}

// Domain-specific errors for e-commerce
func NewOrderNotFound(orderID string) *AppError {
	return &AppError{
		Code:     "ORDER_NOT_FOUND",
		Message:  "Order not found",
		Details:  fmt.Sprintf("Order with ID %s does not exist", orderID),
		HTTPCode: http.StatusNotFound,
	}
}

func NewInsufficientInventory(productID string, requested, available int) *AppError {
	return &AppError{
		Code:     "INSUFFICIENT_INVENTORY",
		Message:  "Insufficient inventory",
		Details:  fmt.Sprintf("Product %s: requested %d, available %d", productID, requested, available),
		HTTPCode: http.StatusConflict,
	}
}

func NewPaymentFailed(reason string) *AppError {
	return &AppError{
		Code:     "PAYMENT_FAILED",
		Message:  "Payment processing failed",
		Details:  reason,
		HTTPCode: http.StatusPaymentRequired,
	}
}

func NewProductNotFound(productID string) *AppError {
	return &AppError{
		Code:     "PRODUCT_NOT_FOUND",
		Message:  "Product not found",
		Details:  fmt.Sprintf("Product with ID %s does not exist", productID),
		HTTPCode: http.StatusNotFound,
	}
}

func NewOrderAlreadyProcessed(orderID string) *AppError {
	return &AppError{
		Code:     "ORDER_ALREADY_PROCESSED",
		Message:  "Order has already been processed",
		Details:  fmt.Sprintf("Order %s cannot be modified in its current state", orderID),
		HTTPCode: http.StatusConflict,
	}
}
