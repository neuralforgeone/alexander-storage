// Package domain contains the core business entities for Alexander Storage.
package domain

import (
	"time"
)

// LifecycleStatus represents the status of a lifecycle rule.
type LifecycleStatus string

const (
	// LifecycleEnabled means the rule is active and will be applied.
	LifecycleEnabled LifecycleStatus = "Enabled"

	// LifecycleDisabled means the rule is inactive.
	LifecycleDisabled LifecycleStatus = "Disabled"
)

// LifecycleRule represents an object lifecycle management rule.
// Currently supports expiration rules only (objects are deleted after N days).
type LifecycleRule struct {
	// ID is the unique database identifier.
	ID int64 `json:"id"`

	// BucketID is the ID of the bucket this rule belongs to.
	BucketID int64 `json:"bucket_id"`

	// RuleID is the user-defined identifier for this rule.
	// Must be unique within the bucket.
	RuleID string `json:"rule_id"`

	// Prefix is the object key prefix filter.
	// Empty string means all objects in the bucket.
	Prefix string `json:"prefix"`

	// ExpirationDays is the number of days after object creation
	// when the object should be deleted. Nil means never expire.
	ExpirationDays *int `json:"expiration_days,omitempty"`

	// Status indicates whether the rule is enabled.
	Status LifecycleStatus `json:"status"`

	// CreatedAt is when the rule was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the rule was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// NewLifecycleRule creates a new lifecycle rule with default values.
func NewLifecycleRule(bucketID int64, ruleID string) *LifecycleRule {
	now := time.Now().UTC()
	return &LifecycleRule{
		BucketID:  bucketID,
		RuleID:    ruleID,
		Prefix:    "",
		Status:    LifecycleEnabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate checks if the lifecycle rule is valid.
func (r *LifecycleRule) Validate() error {
	if r.RuleID == "" {
		return ErrInvalidLifecycleRule
	}
	if len(r.RuleID) > 255 {
		return ErrInvalidLifecycleRule
	}
	if r.Status != LifecycleEnabled && r.Status != LifecycleDisabled {
		return ErrInvalidLifecycleRule
	}
	// Expiration days must be positive if set
	if r.ExpirationDays != nil && *r.ExpirationDays < 1 {
		return ErrInvalidLifecycleRule
	}
	return nil
}

// IsEnabled returns true if the rule is active.
func (r *LifecycleRule) IsEnabled() bool {
	return r.Status == LifecycleEnabled
}

// HasExpiration returns true if the rule has an expiration policy.
func (r *LifecycleRule) HasExpiration() bool {
	return r.ExpirationDays != nil && *r.ExpirationDays > 0
}

// MatchesKey returns true if the given object key matches this rule's prefix filter.
func (r *LifecycleRule) MatchesKey(key string) bool {
	if r.Prefix == "" {
		return true
	}
	return len(key) >= len(r.Prefix) && key[:len(r.Prefix)] == r.Prefix
}

// ShouldExpire checks if an object created at the given time should be expired.
func (r *LifecycleRule) ShouldExpire(createdAt time.Time) bool {
	if !r.HasExpiration() {
		return false
	}

	expirationTime := createdAt.AddDate(0, 0, *r.ExpirationDays)
	return time.Now().UTC().After(expirationTime)
}

// LifecycleConfiguration represents the complete lifecycle configuration for a bucket.
type LifecycleConfiguration struct {
	// Rules is the list of lifecycle rules for the bucket.
	Rules []*LifecycleRule `json:"rules"`
}

// NewLifecycleConfiguration creates an empty lifecycle configuration.
func NewLifecycleConfiguration() *LifecycleConfiguration {
	return &LifecycleConfiguration{
		Rules: make([]*LifecycleRule, 0),
	}
}

// GetEnabledRules returns only the enabled rules.
func (c *LifecycleConfiguration) GetEnabledRules() []*LifecycleRule {
	enabled := make([]*LifecycleRule, 0)
	for _, rule := range c.Rules {
		if rule.IsEnabled() {
			enabled = append(enabled, rule)
		}
	}
	return enabled
}

// FindRule finds a rule by its ID.
func (c *LifecycleConfiguration) FindRule(ruleID string) *LifecycleRule {
	for _, rule := range c.Rules {
		if rule.RuleID == ruleID {
			return rule
		}
	}
	return nil
}
