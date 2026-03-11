package models

import "time"

// PriceCache represents a cached price lookup in the database.
type PriceCache struct {
	ID           string    `json:"id"`
	Asset        string    `json:"asset"`
	Denomination string    `json:"denomination"`
	Timestamp    time.Time `json:"timestamp"`
	Price        float64   `json:"price"`
	Source       string    `json:"source"`
	CreatedAt    time.Time `json:"created_at"`
}

// PriceCacheInput represents input for inserting a price cache entry.
type PriceCacheInput struct {
	Asset        string
	Denomination string
	Timestamp    time.Time
	Price        float64
	Source       string
}
