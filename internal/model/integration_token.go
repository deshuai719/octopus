package model

import "time"

// IntegrationToken is a long-lived, revocable service identity. TokenHash is
// deliberately excluded from JSON; the raw token is never persisted.
type IntegrationToken struct {
	ID         int        `json:"id" gorm:"primaryKey"`
	Name       string     `json:"name" gorm:"type:varchar(100);not null"`
	TokenHash  string     `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	TokenHint  string     `json:"token_hint" gorm:"type:varchar(32);not null"`
	Scopes     []string   `json:"scopes" gorm:"serializer:json;type:text;not null"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" gorm:"index"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
