// Package tiering provides automatic data movement between storage tiers
// based on access patterns and configurable policies.
package tiering

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/cluster"
)

// Common errors for the tiering package.
var (
	ErrNoTargetNode      = errors.New("no suitable target node found")
	ErrTieringInProgress = errors.New("tiering already in progress for this blob")
	ErrInvalidPolicy     = errors.New("invalid tiering policy")
	ErrMigrationFailed   = errors.New("migration failed")
)

// Tier represents a storage tier.
type Tier string

const (
	// TierHot is for frequently accessed data.
	TierHot Tier = "hot"

	// TierWarm is for moderately accessed data.
	TierWarm Tier = "warm"

	// TierCold is for archival/rarely accessed data.
	TierCold Tier = "cold"
)

// PolicyConfig defines rules for automatic tiering.
// This is a simplified policy config used internally; see interfaces.go for the full Policy type.
type PolicyConfig struct {
	// ID is the unique identifier for this policy.
	ID string `json:"id"`

	// Name is a human-readable name for the policy.
	Name string `json:"name"`

	// Enabled indicates if the policy is active.
	Enabled bool `json:"enabled"`

	// HotToWarmDays is days without access before moving from hot to warm.
	HotToWarmDays int `json:"hot_to_warm_days"`

	// WarmToColdDays is days without access before moving from warm to cold.
	WarmToColdDays int `json:"warm_to_cold_days"`

	// MinSize is the minimum blob size in bytes to apply this policy.
	MinSize int64 `json:"min_size"`

	// MaxSize is the maximum blob size in bytes to apply this policy (0 = no limit).
	MaxSize int64 `json:"max_size"`

	// BucketFilter is a regex pattern for bucket names to match (empty = all).
	BucketFilter string `json:"bucket_filter,omitempty"`
}

// DefaultPolicyConfig returns a sensible default tiering policy config.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		ID:             "default",
		Name:           "Default Tiering Policy",
		Enabled:        true,
		HotToWarmDays:  30,
		WarmToColdDays: 90,
		MinSize:        1024 * 1024, // 1MB minimum
	}
}

// BlobAccessInfo contains information about blob access patterns.
type BlobAccessInfo struct {
	// ContentHash is the blob identifier.
	ContentHash string `json:"content_hash"`

	// CurrentTier is the current storage tier.
	CurrentTier Tier `json:"current_tier"`

	// Size is the blob size in bytes.
	Size int64 `json:"size"`

	// CreatedAt is when the blob was created.
	CreatedAt time.Time `json:"created_at"`

	// LastAccessedAt is the last time the blob was accessed.
	LastAccessedAt time.Time `json:"last_accessed_at"`

	// AccessCount is the total number of accesses.
	AccessCount int64 `json:"access_count"`

	// BucketName is the bucket containing this blob (for filtering).
	BucketName string `json:"bucket_name,omitempty"`
}

// TieringDecision represents a decision to move a blob.
type TieringDecision struct {
	// ContentHash is the blob identifier.
	ContentHash string `json:"content_hash"`

	// SourceTier is the current tier.
	SourceTier Tier `json:"source_tier"`

	// TargetTier is the recommended target tier.
	TargetTier Tier `json:"target_tier"`

	// Reason explains why this decision was made.
	Reason string `json:"reason"`

	// Priority is the priority for processing (higher = more urgent).
	Priority int `json:"priority"`

	// PolicyID is the policy that triggered this decision.
	PolicyID string `json:"policy_id"`
}

// MigrationStatus represents the status of a blob migration.
type MigrationStatus struct {
	// ContentHash is the blob identifier.
	ContentHash string `json:"content_hash"`

	// SourceNodeID is the source node.
	SourceNodeID string `json:"source_node_id"`

	// TargetNodeID is the target node.
	TargetNodeID string `json:"target_node_id"`

	// SourceTier is the source tier.
	SourceTier Tier `json:"source_tier"`

	// TargetTier is the target tier.
	TargetTier Tier `json:"target_tier"`

	// Status is the current migration status.
	Status string `json:"status"` // "pending", "in_progress", "completed", "failed"

	// StartedAt is when the migration started.
	StartedAt time.Time `json:"started_at,omitempty"`

	// CompletedAt is when the migration completed.
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// Error is the error message if failed.
	Error string `json:"error,omitempty"`

	// BytesTransferred is the number of bytes transferred.
	BytesTransferred int64 `json:"bytes_transferred"`
}

// AccessTracker tracks blob access patterns.
type AccessTracker interface {
	// RecordAccess records an access to a blob.
	RecordAccess(ctx context.Context, contentHash string) error

	// GetAccessInfo returns access information for a blob.
	GetAccessInfo(ctx context.Context, contentHash string) (*BlobAccessInfo, error)

	// GetBlobsForTiering returns blobs that may need tiering based on access patterns.
	GetBlobsForTiering(ctx context.Context, policy PolicyConfig, limit int) ([]*BlobAccessInfo, error)
}

// ControllerConfig contains configuration for the tiering controller.
type ControllerConfig struct {
	// ScanInterval is how often to scan for tiering candidates.
	ScanInterval time.Duration

	// MaxConcurrentMigrations is the maximum number of simultaneous migrations.
	MaxConcurrentMigrations int

	// MigrationBatchSize is the number of blobs to process per scan.
	MigrationBatchSize int

	// RetryDelay is the delay before retrying a failed migration.
	RetryDelay time.Duration

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int
}

// DefaultControllerConfig returns sensible defaults.
func DefaultControllerConfig() ControllerConfig {
	return ControllerConfig{
		ScanInterval:            time.Hour,
		MaxConcurrentMigrations: 5,
		MigrationBatchSize:      100,
		RetryDelay:              5 * time.Minute,
		MaxRetries:              3,
	}
}

// TieringController manages automatic tiering of data between storage tiers.
// It implements the Controller interface from interfaces.go with a simplified internal policy config.
type TieringController struct {
	config        ControllerConfig
	logger        zerolog.Logger
	clusterMgr    cluster.ClusterManager
	nodeSelector  cluster.NodeSelector
	accessTracker AccessTracker

	// Policies
	policiesMu sync.RWMutex
	policies   map[string]PolicyConfig

	// In-flight migrations
	migrationsMu sync.RWMutex
	migrations   map[string]*MigrationStatus // contentHash -> status

	// Migration semaphore
	migrationSem chan struct{}

	// Shutdown
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// NewTieringController creates a new tiering controller.
func NewTieringController(
	config ControllerConfig,
	clusterMgr cluster.ClusterManager,
	nodeSelector cluster.NodeSelector,
	accessTracker AccessTracker,
	logger zerolog.Logger,
) *TieringController {
	if config.ScanInterval <= 0 {
		config.ScanInterval = DefaultControllerConfig().ScanInterval
	}
	if config.MaxConcurrentMigrations <= 0 {
		config.MaxConcurrentMigrations = DefaultControllerConfig().MaxConcurrentMigrations
	}
	if config.MigrationBatchSize <= 0 {
		config.MigrationBatchSize = DefaultControllerConfig().MigrationBatchSize
	}

	c := &TieringController{
		config:        config,
		logger:        logger.With().Str("component", "tiering-controller").Logger(),
		clusterMgr:    clusterMgr,
		nodeSelector:  nodeSelector,
		accessTracker: accessTracker,
		policies:      make(map[string]PolicyConfig),
		migrations:    make(map[string]*MigrationStatus),
		migrationSem:  make(chan struct{}, config.MaxConcurrentMigrations),
		shutdownCh:    make(chan struct{}),
	}

	// Add default policy
	c.policies["default"] = DefaultPolicyConfig()

	return c
}

// Start begins the tiering controller's background processing.
func (c *TieringController) Start(ctx context.Context) error {
	c.logger.Info().
		Dur("scan_interval", c.config.ScanInterval).
		Int("max_concurrent_migrations", c.config.MaxConcurrentMigrations).
		Msg("Starting tiering controller")

	c.wg.Add(1)
	go c.scanLoop(ctx)

	return nil
}

// Stop gracefully shuts down the controller.
func (c *TieringController) Stop() error {
	c.logger.Info().Msg("Stopping tiering controller")
	close(c.shutdownCh)
	c.wg.Wait()
	return nil
}

// scanLoop periodically scans for tiering candidates.
func (c *TieringController) scanLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.ScanInterval)
	defer ticker.Stop()

	// Run initial scan
	c.scan(ctx)

	for {
		select {
		case <-c.shutdownCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scan(ctx)
		}
	}
}

// scan performs a single scan for tiering candidates.
func (c *TieringController) scan(ctx context.Context) {
	c.logger.Debug().Msg("Starting tiering scan")

	c.policiesMu.RLock()
	policies := make([]PolicyConfig, 0, len(c.policies))
	for _, p := range c.policies {
		if p.Enabled {
			policies = append(policies, p)
		}
	}
	c.policiesMu.RUnlock()

	for _, policy := range policies {
		select {
		case <-c.shutdownCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		c.processPolicy(ctx, policy)
	}

	c.logger.Debug().Msg("Tiering scan completed")
}

// processPolicy evaluates and executes a single policy.
func (c *TieringController) processPolicy(ctx context.Context, policy PolicyConfig) {
	blobs, err := c.accessTracker.GetBlobsForTiering(ctx, policy, c.config.MigrationBatchSize)
	if err != nil {
		c.logger.Error().Err(err).Str("policy_id", policy.ID).Msg("Failed to get blobs for tiering")
		return
	}

	for _, blob := range blobs {
		select {
		case <-c.shutdownCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		decision := c.evaluateBlob(blob, policy)
		if decision != nil {
			c.executeTiering(ctx, decision)
		}
	}
}

// evaluateBlob evaluates a blob against a policy and returns a tiering decision.
func (c *TieringController) evaluateBlob(blob *BlobAccessInfo, policy PolicyConfig) *TieringDecision {
	now := time.Now()
	daysSinceAccess := int(now.Sub(blob.LastAccessedAt).Hours() / 24)

	var targetTier Tier
	var reason string

	switch blob.CurrentTier {
	case TierHot:
		if daysSinceAccess >= policy.HotToWarmDays {
			targetTier = TierWarm
			reason = "No access for " + string(rune(daysSinceAccess)) + " days (threshold: " + string(rune(policy.HotToWarmDays)) + ")"
		}
	case TierWarm:
		if daysSinceAccess >= policy.WarmToColdDays {
			targetTier = TierCold
			reason = "No access for " + string(rune(daysSinceAccess)) + " days (threshold: " + string(rune(policy.WarmToColdDays)) + ")"
		}
	case TierCold:
		// Already in coldest tier, no action needed
		return nil
	}

	if targetTier == "" {
		return nil
	}

	// Check if migration is already in progress
	c.migrationsMu.RLock()
	_, inProgress := c.migrations[blob.ContentHash]
	c.migrationsMu.RUnlock()

	if inProgress {
		return nil
	}

	return &TieringDecision{
		ContentHash: blob.ContentHash,
		SourceTier:  blob.CurrentTier,
		TargetTier:  targetTier,
		Reason:      reason,
		Priority:    daysSinceAccess, // Higher = older = higher priority
		PolicyID:    policy.ID,
	}
}

// executeTiering executes a tiering decision.
func (c *TieringController) executeTiering(ctx context.Context, decision *TieringDecision) {
	// Acquire migration semaphore
	select {
	case c.migrationSem <- struct{}{}:
	case <-c.shutdownCh:
		return
	case <-ctx.Done():
		return
	}

	// Run migration in background
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() { <-c.migrationSem }()

		c.migrateBlob(ctx, decision)
	}()
}

// migrateBlob performs the actual migration of a blob.
func (c *TieringController) migrateBlob(ctx context.Context, decision *TieringDecision) {
	logger := c.logger.With().
		Str("content_hash", decision.ContentHash).
		Str("source_tier", string(decision.SourceTier)).
		Str("target_tier", string(decision.TargetTier)).
		Logger()

	// Create migration status
	status := &MigrationStatus{
		ContentHash: decision.ContentHash,
		SourceTier:  decision.SourceTier,
		TargetTier:  decision.TargetTier,
		Status:      "pending",
	}

	c.migrationsMu.Lock()
	c.migrations[decision.ContentHash] = status
	c.migrationsMu.Unlock()

	defer func() {
		// Keep completed/failed status for a while before removing
		time.AfterFunc(5*time.Minute, func() {
			c.migrationsMu.Lock()
			delete(c.migrations, decision.ContentHash)
			c.migrationsMu.Unlock()
		})
	}()

	// Find target node
	targetRole := cluster.NodeRole(decision.TargetTier)
	targetNode, err := c.nodeSelector.SelectForTiering(ctx, decision.ContentHash, targetRole)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to select target node")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}

	if targetNode == nil {
		logger.Warn().Msg("No suitable target node found")
		status.Status = "failed"
		status.Error = ErrNoTargetNode.Error()
		return
	}

	status.TargetNodeID = targetNode.ID
	status.Status = "in_progress"
	status.StartedAt = time.Now()

	logger.Info().
		Str("target_node", targetNode.ID).
		Msg("Starting blob migration")

	// Get source locations
	locations, err := c.clusterMgr.GetBlobLocations(ctx, decision.ContentHash)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get blob locations")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}
	if len(locations) == 0 {
		logger.Error().Msg("No source locations found for blob")
		status.Status = "failed"
		status.Error = "no source locations"
		return
	}

	// Find a healthy source node
	var sourceClient cluster.NodeClient
	var sourceNodeID string

	for _, loc := range locations {
		if loc.NodeID == targetNode.ID {
			continue // Don't copy from target to itself
		}

		client, err := c.clusterMgr.GetClientForNode(ctx, loc.NodeID)
		if err == nil {
			sourceClient = client
			sourceNodeID = loc.NodeID
			break
		}
	}

	if sourceClient == nil {
		logger.Error().Msg("No healthy source node found")
		status.Status = "failed"
		status.Error = "no healthy source node"
		return
	}

	status.SourceNodeID = sourceNodeID

	// Retrieve blob from source
	reader, err := sourceClient.RetrieveBlob(ctx, decision.ContentHash)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to retrieve blob from source")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}
	defer reader.Close()

	// Get target client
	targetClient, err := c.clusterMgr.GetClientForNode(ctx, targetNode.ID)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get target client")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}

	// Get blob size
	accessInfo, err := c.accessTracker.GetAccessInfo(ctx, decision.ContentHash)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get blob access info")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}

	// Transfer blob to target
	err = targetClient.TransferBlob(ctx, decision.ContentHash, accessInfo.Size, reader)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to transfer blob to target")
		status.Status = "failed"
		status.Error = err.Error()
		return
	}

	status.BytesTransferred = accessInfo.Size

	// Register new location
	newLocation := &cluster.BlobLocation{
		ContentHash: decision.ContentHash,
		NodeID:      targetNode.ID,
		IsPrimary:   false,
		SyncedAt:    time.Now(),
	}
	c.clusterMgr.RegisterBlobLocation(ctx, newLocation)

	status.Status = "completed"
	status.CompletedAt = time.Now()

	logger.Info().
		Int64("bytes_transferred", status.BytesTransferred).
		Dur("duration", status.CompletedAt.Sub(status.StartedAt)).
		Msg("Blob migration completed")
}

// AddPolicy adds or updates a tiering policy.
func (c *TieringController) AddPolicy(policy PolicyConfig) error {
	if policy.ID == "" {
		return ErrInvalidPolicy
	}

	c.policiesMu.Lock()
	c.policies[policy.ID] = policy
	c.policiesMu.Unlock()

	c.logger.Info().
		Str("policy_id", policy.ID).
		Str("policy_name", policy.Name).
		Msg("Tiering policy added/updated")

	return nil
}

// RemovePolicy removes a tiering policy.
func (c *TieringController) RemovePolicy(policyID string) error {
	c.policiesMu.Lock()
	delete(c.policies, policyID)
	c.policiesMu.Unlock()

	c.logger.Info().Str("policy_id", policyID).Msg("Tiering policy removed")
	return nil
}

// GetPolicies returns all tiering policies.
func (c *TieringController) GetPolicies() []PolicyConfig {
	c.policiesMu.RLock()
	defer c.policiesMu.RUnlock()

	result := make([]PolicyConfig, 0, len(c.policies))
	for _, p := range c.policies {
		result = append(result, p)
	}
	return result
}

// GetMigrationStatus returns the status of a blob migration.
func (c *TieringController) GetMigrationStatus(contentHash string) (*MigrationStatus, bool) {
	c.migrationsMu.RLock()
	defer c.migrationsMu.RUnlock()

	status, exists := c.migrations[contentHash]
	if !exists {
		return nil, false
	}

	statusCopy := *status
	return &statusCopy, true
}

// GetActiveMigrations returns all active migrations.
func (c *TieringController) GetActiveMigrations() []*MigrationStatus {
	c.migrationsMu.RLock()
	defer c.migrationsMu.RUnlock()

	var result []*MigrationStatus
	for _, status := range c.migrations {
		if status.Status == "pending" || status.Status == "in_progress" {
			statusCopy := *status
			result = append(result, &statusCopy)
		}
	}
	return result
}

// TriggerScan manually triggers a tiering scan.
func (c *TieringController) TriggerScan(ctx context.Context) {
	c.logger.Info().Msg("Manual tiering scan triggered")
	go c.scan(ctx)
}

// ForceMove immediately moves a blob to a specific tier.
func (c *TieringController) ForceMove(ctx context.Context, contentHash string, targetTier Tier) error {
	// Check if migration is already in progress
	c.migrationsMu.RLock()
	_, inProgress := c.migrations[contentHash]
	c.migrationsMu.RUnlock()

	if inProgress {
		return ErrTieringInProgress
	}

	// Get current blob info
	accessInfo, err := c.accessTracker.GetAccessInfo(ctx, contentHash)
	if err != nil {
		return err
	}

	decision := &TieringDecision{
		ContentHash: contentHash,
		SourceTier:  accessInfo.CurrentTier,
		TargetTier:  targetTier,
		Reason:      "Manual force move",
		Priority:    100, // High priority
		PolicyID:    "manual",
	}

	// Execute synchronously
	c.migrateBlob(ctx, decision)

	// Check result
	c.migrationsMu.RLock()
	status, exists := c.migrations[contentHash]
	c.migrationsMu.RUnlock()

	if !exists {
		return ErrMigrationFailed
	}

	if status.Status == "failed" {
		return errors.New(status.Error)
	}

	return nil
}
