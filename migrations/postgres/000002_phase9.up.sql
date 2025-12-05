-- Alexander Storage Database Schema
-- Migration: 000002_phase9
-- Description: Phase 9 features - ACLs, SSE, Sessions, Lifecycle Rules

-- ============================================
-- CUSTOM TYPES
-- ============================================

-- Bucket ACL type (Canned ACLs)
CREATE TYPE bucket_acl AS ENUM ('private', 'public-read', 'public-read-write');

-- Lifecycle rule status
CREATE TYPE lifecycle_status AS ENUM ('Enabled', 'Disabled');

-- ============================================
-- BUCKETS TABLE - Add ACL column
-- ============================================
ALTER TABLE buckets 
ADD COLUMN acl bucket_acl NOT NULL DEFAULT 'private';

-- Index for public bucket lookups (useful for anonymous access checks)
CREATE INDEX idx_buckets_acl ON buckets (acl) WHERE acl != 'private';

-- ============================================
-- BLOBS TABLE - Add encryption flag
-- ============================================
ALTER TABLE blobs 
ADD COLUMN is_encrypted BOOLEAN NOT NULL DEFAULT FALSE;

-- Index for migration tool to find unencrypted blobs
CREATE INDEX idx_blobs_unencrypted ON blobs (is_encrypted, created_at) 
WHERE is_encrypted = FALSE;

-- ============================================
-- SESSIONS TABLE (for Dashboard authentication)
-- ============================================
CREATE TABLE sessions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         BIGINT NOT NULL,
    token           VARCHAR(64) NOT NULL,          -- Secure random token
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip_address      VARCHAR(45),                   -- IPv4 or IPv6
    user_agent      TEXT,
    
    -- Foreign keys
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) 
        REFERENCES users(id) ON DELETE CASCADE,
    
    -- Constraints
    CONSTRAINT sessions_token_unique UNIQUE (token)
);

-- Indexes for sessions
CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_token ON sessions (token);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

-- Index for expired session cleanup
CREATE INDEX idx_sessions_cleanup ON sessions (expires_at) 
WHERE expires_at < NOW();

-- ============================================
-- LIFECYCLE RULES TABLE
-- ============================================
CREATE TABLE lifecycle_rules (
    id                  BIGSERIAL PRIMARY KEY,
    bucket_id           BIGINT NOT NULL,
    rule_id             VARCHAR(255) NOT NULL,      -- User-defined rule identifier
    prefix              VARCHAR(1024) DEFAULT '',   -- Object key prefix filter
    expiration_days     INTEGER,                    -- Days until expiration (NULL = never)
    status              lifecycle_status NOT NULL DEFAULT 'Enabled',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Foreign keys
    CONSTRAINT fk_lifecycle_rules_bucket FOREIGN KEY (bucket_id) 
        REFERENCES buckets(id) ON DELETE CASCADE,
    
    -- Constraints
    CONSTRAINT lifecycle_rules_bucket_rule_unique UNIQUE (bucket_id, rule_id),
    CONSTRAINT lifecycle_rules_expiration_positive CHECK (expiration_days IS NULL OR expiration_days > 0)
);

-- Indexes for lifecycle_rules
CREATE INDEX idx_lifecycle_rules_bucket ON lifecycle_rules (bucket_id);
CREATE INDEX idx_lifecycle_rules_enabled ON lifecycle_rules (bucket_id, status) 
WHERE status = 'Enabled';

-- Trigger to update updated_at
CREATE TRIGGER lifecycle_rules_updated_at
    BEFORE UPDATE ON lifecycle_rules
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- HELPER FUNCTIONS
-- ============================================

-- Function to check if a bucket allows anonymous read access
CREATE OR REPLACE FUNCTION bucket_allows_anonymous_read(p_bucket_name VARCHAR(63))
RETURNS BOOLEAN AS $$
DECLARE
    v_acl bucket_acl;
BEGIN
    SELECT acl INTO v_acl FROM buckets WHERE name = p_bucket_name;
    RETURN v_acl IN ('public-read', 'public-read-write');
END;
$$ LANGUAGE plpgsql;

-- Function to check if a bucket allows anonymous write access
CREATE OR REPLACE FUNCTION bucket_allows_anonymous_write(p_bucket_name VARCHAR(63))
RETURNS BOOLEAN AS $$
DECLARE
    v_acl bucket_acl;
BEGIN
    SELECT acl INTO v_acl FROM buckets WHERE name = p_bucket_name;
    RETURN v_acl = 'public-read-write';
END;
$$ LANGUAGE plpgsql;

-- Function to clean up expired sessions (called by session cleanup job)
CREATE OR REPLACE FUNCTION cleanup_expired_sessions()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM sessions WHERE expires_at < NOW();
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Function to get objects due for expiration based on lifecycle rules
CREATE OR REPLACE FUNCTION get_objects_for_expiration(p_limit INTEGER DEFAULT 1000)
RETURNS TABLE(
    object_id BIGINT,
    bucket_id BIGINT,
    key VARCHAR(1024),
    version_id UUID,
    content_hash CHAR(64),
    rule_id VARCHAR(255)
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        o.id AS object_id,
        o.bucket_id,
        o.key,
        o.version_id,
        o.content_hash,
        lr.rule_id
    FROM objects o
    INNER JOIN lifecycle_rules lr ON o.bucket_id = lr.bucket_id
    WHERE lr.status = 'Enabled'
      AND lr.expiration_days IS NOT NULL
      AND o.is_latest = TRUE
      AND o.is_delete_marker = FALSE
      AND o.created_at < NOW() - (lr.expiration_days || ' days')::INTERVAL
      AND (lr.prefix = '' OR o.key LIKE lr.prefix || '%')
    ORDER BY o.created_at ASC
    LIMIT p_limit;
END;
$$ LANGUAGE plpgsql;
