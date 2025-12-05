-- Alexander Storage Database Schema for SQLite
-- Migration: 000002_phase9
-- Description: Phase 9 rollback - Remove ACLs, SSE, Sessions, Lifecycle Rules

-- Drop trigger
DROP TRIGGER IF EXISTS lifecycle_rules_updated_at;

-- Drop lifecycle_rules table
DROP TABLE IF EXISTS lifecycle_rules;

-- Drop sessions table  
DROP TABLE IF EXISTS sessions;

-- SQLite doesn't support DROP COLUMN easily
-- We need to recreate the tables without the new columns

-- Recreate blobs table without is_encrypted
CREATE TABLE blobs_backup AS SELECT 
    content_hash, size, storage_path, ref_count, created_at, last_accessed 
FROM blobs;
DROP TABLE blobs;
CREATE TABLE blobs (
    content_hash    TEXT PRIMARY KEY,
    size            INTEGER NOT NULL,
    storage_path    TEXT NOT NULL,
    ref_count       INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    last_accessed   TEXT NOT NULL DEFAULT (datetime('now')),
    CONSTRAINT blobs_ref_count_non_negative CHECK (ref_count >= 0),
    CONSTRAINT blobs_size_non_negative CHECK (size >= 0),
    CONSTRAINT blobs_content_hash_length CHECK (length(content_hash) = 64)
);
INSERT INTO blobs SELECT * FROM blobs_backup;
DROP TABLE blobs_backup;
CREATE INDEX IF NOT EXISTS idx_blobs_orphan ON blobs (ref_count, created_at) WHERE ref_count = 0;
CREATE INDEX IF NOT EXISTS idx_blobs_last_accessed ON blobs (last_accessed);

-- Recreate buckets table without acl
CREATE TABLE buckets_backup AS SELECT 
    id, owner_id, name, region, versioning, object_lock, created_at 
FROM buckets;
DROP TABLE buckets;
CREATE TABLE buckets (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id        INTEGER NOT NULL,
    name            TEXT NOT NULL,
    region          TEXT NOT NULL DEFAULT 'us-east-1',
    versioning      TEXT NOT NULL DEFAULT 'Disabled' CHECK (versioning IN ('Disabled', 'Enabled', 'Suspended')),
    object_lock     INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT buckets_name_unique UNIQUE (name),
    CONSTRAINT buckets_name_length CHECK (length(name) >= 3 AND length(name) <= 63)
);
INSERT INTO buckets SELECT * FROM buckets_backup;
DROP TABLE buckets_backup;
CREATE INDEX IF NOT EXISTS idx_buckets_owner_id ON buckets (owner_id);
CREATE INDEX IF NOT EXISTS idx_buckets_name ON buckets (name);
