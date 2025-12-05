-- Alexander Storage Database Schema for SQLite
-- Migration: 000002_phase9
-- Description: Phase 9 features - ACLs, SSE, Sessions, Lifecycle Rules
-- 
-- SQLite Notes:
-- - No ALTER TABLE ADD COLUMN with DEFAULT for existing rows, use separate UPDATE
-- - No ENUM types, use TEXT with CHECK constraints
-- - No stored procedures, handle in application code

-- ============================================
-- BUCKETS TABLE - Add ACL column
-- ============================================
-- SQLite requires creating a new table to add column with constraints
-- Using a simpler approach with ALTER TABLE (SQLite 3.35+)
ALTER TABLE buckets ADD COLUMN acl TEXT NOT NULL DEFAULT 'private' 
    CHECK (acl IN ('private', 'public-read', 'public-read-write'));

-- Index for public bucket lookups
CREATE INDEX IF NOT EXISTS idx_buckets_acl ON buckets (acl) WHERE acl != 'private';

-- ============================================
-- BLOBS TABLE - Add encryption flag
-- ============================================
ALTER TABLE blobs ADD COLUMN is_encrypted INTEGER NOT NULL DEFAULT 0;

-- Index for migration tool to find unencrypted blobs
CREATE INDEX IF NOT EXISTS idx_blobs_unencrypted ON blobs (is_encrypted, created_at) 
WHERE is_encrypted = 0;

-- ============================================
-- SESSIONS TABLE (for Dashboard authentication)
-- ============================================
CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,              -- UUID as text
    user_id         INTEGER NOT NULL,
    token           TEXT NOT NULL,                 -- Secure random token
    expires_at      TEXT NOT NULL,                 -- ISO8601 datetime
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    ip_address      TEXT,                          -- IPv4 or IPv6
    user_agent      TEXT,
    
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT sessions_token_unique UNIQUE (token)
);

-- Indexes for sessions
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions (token);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions (expires_at);

-- ============================================
-- LIFECYCLE RULES TABLE
-- ============================================
CREATE TABLE IF NOT EXISTS lifecycle_rules (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_id           INTEGER NOT NULL,
    rule_id             TEXT NOT NULL,              -- User-defined rule identifier
    prefix              TEXT DEFAULT '',            -- Object key prefix filter
    expiration_days     INTEGER,                    -- Days until expiration (NULL = never)
    status              TEXT NOT NULL DEFAULT 'Enabled' CHECK (status IN ('Enabled', 'Disabled')),
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    
    FOREIGN KEY (bucket_id) REFERENCES buckets(id) ON DELETE CASCADE,
    CONSTRAINT lifecycle_rules_bucket_rule_unique UNIQUE (bucket_id, rule_id),
    CONSTRAINT lifecycle_rules_expiration_positive CHECK (expiration_days IS NULL OR expiration_days > 0)
);

-- Indexes for lifecycle_rules
CREATE INDEX IF NOT EXISTS idx_lifecycle_rules_bucket ON lifecycle_rules (bucket_id);
CREATE INDEX IF NOT EXISTS idx_lifecycle_rules_enabled ON lifecycle_rules (bucket_id, status) 
WHERE status = 'Enabled';

-- Trigger to update updated_at
CREATE TRIGGER IF NOT EXISTS lifecycle_rules_updated_at
    AFTER UPDATE ON lifecycle_rules
    FOR EACH ROW
    BEGIN
        UPDATE lifecycle_rules SET updated_at = datetime('now') WHERE id = NEW.id;
    END;
