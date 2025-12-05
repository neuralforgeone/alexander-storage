// Package domain contains the core business entities for Alexander Storage.
package domain

import (
	"regexp"
	"time"
)

// VersioningStatus represents the versioning state of a bucket.
type VersioningStatus string

const (
	// VersioningDisabled means versioning has never been enabled.
	// Objects are overwritten on PUT, deleted on DELETE.
	VersioningDisabled VersioningStatus = "Disabled"

	// VersioningEnabled means versioning is active.
	// Each PUT creates a new version, DELETE creates a delete marker.
	VersioningEnabled VersioningStatus = "Enabled"

	// VersioningSuspended means versioning was enabled but is now paused.
	// New objects get "null" version ID, existing versions are preserved.
	VersioningSuspended VersioningStatus = "Suspended"
)

// BucketACL represents the canned ACL for a bucket.
// This is a simplified access control model supporting three modes.
type BucketACL string

const (
	// ACLPrivate means only the bucket owner can read and write (default).
	ACLPrivate BucketACL = "private"

	// ACLPublicRead means anyone can read, but only owner can write.
	ACLPublicRead BucketACL = "public-read"

	// ACLPublicReadWrite means anyone can read and write (use with caution).
	ACLPublicReadWrite BucketACL = "public-read-write"
)

// ValidBucketACLs is the list of valid ACL values.
var ValidBucketACLs = []BucketACL{ACLPrivate, ACLPublicRead, ACLPublicReadWrite}

// IsValidACL checks if the given ACL string is valid.
func IsValidACL(acl string) bool {
	switch BucketACL(acl) {
	case ACLPrivate, ACLPublicRead, ACLPublicReadWrite:
		return true
	default:
		return false
	}
}

// AllowsAnonymousRead returns true if the ACL allows unauthenticated read access.
func (a BucketACL) AllowsAnonymousRead() bool {
	return a == ACLPublicRead || a == ACLPublicReadWrite
}

// AllowsAnonymousWrite returns true if the ACL allows unauthenticated write access.
func (a BucketACL) AllowsAnonymousWrite() bool {
	return a == ACLPublicReadWrite
}

// bucketNameRegex validates S3-compliant bucket names.
// Rules: 3-63 characters, lowercase letters, numbers, hyphens, periods.
// Must start and end with letter or number.
var bucketNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// Bucket represents an S3-compatible storage bucket.
// Buckets are containers for objects and define policies like versioning.
type Bucket struct {
	// ID is the unique identifier for the bucket.
	ID int64 `json:"id"`

	// OwnerID is the ID of the user who owns this bucket.
	OwnerID int64 `json:"owner_id"`

	// Name is the globally unique bucket name.
	// Constraints: 3-63 characters, lowercase, alphanumeric with hyphens/periods.
	Name string `json:"name"`

	// Region is the geographic region where the bucket is located.
	// Default: "us-east-1"
	Region string `json:"region"`

	// Versioning indicates the bucket's versioning status.
	Versioning VersioningStatus `json:"versioning"`

	// ACL is the canned access control list for the bucket.
	// Controls anonymous access permissions.
	ACL BucketACL `json:"acl"`

	// ObjectLock indicates whether object locking (WORM) is enabled.
	// Once enabled, cannot be disabled.
	ObjectLock bool `json:"object_lock"`

	// CreatedAt is the timestamp when the bucket was created.
	CreatedAt time.Time `json:"created_at"`
}

// NewBucket creates a new Bucket with default values.
func NewBucket(ownerID int64, name string) *Bucket {
	return &Bucket{
		OwnerID:    ownerID,
		Name:       name,
		Region:     "us-east-1",
		Versioning: VersioningDisabled,
		ACL:        ACLPrivate,
		ObjectLock: false,
		CreatedAt:  time.Now().UTC(),
	}
}

// IsVersioningEnabled returns true if versioning is currently active.
func (b *Bucket) IsVersioningEnabled() bool {
	return b.Versioning == VersioningEnabled
}

// IsVersioningEverEnabled returns true if versioning was ever enabled.
func (b *Bucket) IsVersioningEverEnabled() bool {
	return b.Versioning == VersioningEnabled || b.Versioning == VersioningSuspended
}

// ValidateName checks if the bucket name follows S3 naming conventions.
func ValidateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return ErrBucketNameLength
	}

	if !bucketNameRegex.MatchString(name) {
		return ErrBucketNameFormat
	}

	// Additional checks for IP-like names
	if isIPAddress(name) {
		return ErrBucketNameIPFormat
	}

	return nil
}

// isIPAddress checks if the string looks like an IP address.
func isIPAddress(s string) bool {
	ipRegex := regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	return ipRegex.MatchString(s)
}
