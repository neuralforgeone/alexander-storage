-- Alexander Storage Database Schema
-- Migration: 000002_phase9
-- Description: Phase 9 rollback - Remove ACLs, SSE, Sessions, Lifecycle Rules

-- Drop helper functions
DROP FUNCTION IF EXISTS get_objects_for_expiration(INTEGER);
DROP FUNCTION IF EXISTS cleanup_expired_sessions();
DROP FUNCTION IF EXISTS bucket_allows_anonymous_write(VARCHAR);
DROP FUNCTION IF EXISTS bucket_allows_anonymous_read(VARCHAR);

-- Drop trigger
DROP TRIGGER IF EXISTS lifecycle_rules_updated_at ON lifecycle_rules;

-- Drop lifecycle_rules table
DROP TABLE IF EXISTS lifecycle_rules;

-- Drop sessions table
DROP TABLE IF EXISTS sessions;

-- Remove blobs.is_encrypted column and index
DROP INDEX IF EXISTS idx_blobs_unencrypted;
ALTER TABLE blobs DROP COLUMN IF EXISTS is_encrypted;

-- Remove buckets.acl column and index
DROP INDEX IF EXISTS idx_buckets_acl;
ALTER TABLE buckets DROP COLUMN IF EXISTS acl;

-- Drop custom types
DROP TYPE IF EXISTS lifecycle_status;
DROP TYPE IF EXISTS bucket_acl;
