-- Alexander Storage v2.0 - Fusion Engine Migration (Rollback)

-- Drop views
DROP VIEW IF EXISTS migration_overview;
DROP VIEW IF EXISTS cluster_health;

-- Drop trigger before dependent function
DROP TRIGGER IF EXISTS trg_update_blob_access_stats ON blob_access_log;

-- Drop functions
DROP FUNCTION IF EXISTS decrement_chunk_ref(VARCHAR);
DROP FUNCTION IF EXISTS increment_chunk_ref(VARCHAR);
DROP FUNCTION IF EXISTS update_blob_access_stats();

-- Drop migration tracking
DROP TABLE IF EXISTS migration_progress;

-- Drop access tracking
DROP TABLE IF EXISTS blob_access_stats;
DROP TABLE IF EXISTS blob_access_log;

-- Drop cluster management
DROP TABLE IF EXISTS blob_locations;
DROP TABLE IF EXISTS cluster_nodes;

-- Drop CDC chunks
DROP TABLE IF EXISTS blob_chunks;
DROP TABLE IF EXISTS cdc_chunks;

-- Drop delta instructions
DROP TABLE IF EXISTS delta_instructions;
DROP TABLE IF EXISTS blob_deltas;

-- Drop composite blob parts
DROP TABLE IF EXISTS blob_parts;

-- Drop blob enhancements
DROP INDEX IF EXISTS idx_blobs_delta_base_hash;
DROP INDEX IF EXISTS idx_blobs_blob_type;
ALTER TABLE blobs DROP COLUMN IF EXISTS delta_base_hash;
ALTER TABLE blobs DROP COLUMN IF EXISTS encryption_scheme;
ALTER TABLE blobs DROP COLUMN IF EXISTS blob_type;
