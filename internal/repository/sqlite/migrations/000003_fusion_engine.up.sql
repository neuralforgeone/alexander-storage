-- Migration: 000003_fusion_engine (SQLite)
-- Adds blob columns required by repository layer

ALTER TABLE blobs ADD COLUMN encryption_iv TEXT;
ALTER TABLE blobs ADD COLUMN blob_type TEXT NOT NULL DEFAULT 'single';
ALTER TABLE blobs ADD COLUMN encryption_scheme TEXT;
ALTER TABLE blobs ADD COLUMN delta_base_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_blobs_blob_type ON blobs (blob_type);