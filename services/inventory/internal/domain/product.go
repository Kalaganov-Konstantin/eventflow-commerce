// Package domain holds the inventory aggregates and their business rules.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Product is a sellable item in the catalog. Money fields are integer minor units (cents).
type Product struct {
	ID          uuid.UUID
	Name        string
	Description string
	SKU         string
	Category    string
	Brand       string
	PriceCents  int64
	CostCents   int64
	Currency    string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}
