#!/bin/bash
# AWS CLI Compatibility Tests for Alexander Storage
# Tests S3 operations using the official AWS CLI

set -e

# Configuration
ENDPOINT_URL="${ALEXANDER_URL:-http://localhost:8080}"
BUCKET_NAME="awscli-test-$(date +%s)"
TEST_FILE="/tmp/test-file-$$"
DOWNLOAD_FILE="/tmp/download-file-$$"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
log_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

cleanup() {
    log_info "Cleaning up..."
    rm -f "$TEST_FILE" "$DOWNLOAD_FILE"
    # Try to delete bucket and its contents
    aws s3 rb "s3://$BUCKET_NAME" --force --endpoint-url "$ENDPOINT_URL" 2>/dev/null || true
}

# Set trap for cleanup
trap cleanup EXIT

# Generate test data
generate_test_file() {
    local size=$1
    dd if=/dev/urandom of="$TEST_FILE" bs=$size count=1 2>/dev/null
    log_info "Generated $size bytes test file"
}

# ===== BUCKET TESTS =====

test_create_bucket() {
    log_info "Testing: Create bucket"
    if aws s3 mb "s3://$BUCKET_NAME" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Create bucket"
    else
        log_fail "Create bucket"
        return 1
    fi
}

test_list_buckets() {
    log_info "Testing: List buckets"
    if aws s3 ls --endpoint-url "$ENDPOINT_URL" | grep -q "$BUCKET_NAME"; then
        log_pass "List buckets (bucket found)"
    else
        log_fail "List buckets (bucket not found)"
        return 1
    fi
}

test_head_bucket() {
    log_info "Testing: Head bucket"
    if aws s3api head-bucket --bucket "$BUCKET_NAME" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Head bucket"
    else
        log_fail "Head bucket"
        return 1
    fi
}

# ===== OBJECT TESTS =====

test_put_object() {
    log_info "Testing: Put object"
    generate_test_file 1024
    if aws s3 cp "$TEST_FILE" "s3://$BUCKET_NAME/test-object" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Put object"
    else
        log_fail "Put object"
        return 1
    fi
}

test_get_object() {
    log_info "Testing: Get object"
    if aws s3 cp "s3://$BUCKET_NAME/test-object" "$DOWNLOAD_FILE" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        # Verify content
        if diff "$TEST_FILE" "$DOWNLOAD_FILE" > /dev/null 2>&1; then
            log_pass "Get object (content verified)"
        else
            log_fail "Get object (content mismatch)"
            return 1
        fi
    else
        log_fail "Get object"
        return 1
    fi
}

test_head_object() {
    log_info "Testing: Head object"
    if aws s3api head-object --bucket "$BUCKET_NAME" --key "test-object" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Head object"
    else
        log_fail "Head object"
        return 1
    fi
}

test_list_objects() {
    log_info "Testing: List objects"
    if aws s3 ls "s3://$BUCKET_NAME" --endpoint-url "$ENDPOINT_URL" | grep -q "test-object"; then
        log_pass "List objects"
    else
        log_fail "List objects"
        return 1
    fi
}

test_copy_object() {
    log_info "Testing: Copy object"
    if aws s3 cp "s3://$BUCKET_NAME/test-object" "s3://$BUCKET_NAME/test-object-copy" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Copy object"
    else
        log_fail "Copy object"
        return 1
    fi
}

test_delete_object() {
    log_info "Testing: Delete object"
    if aws s3 rm "s3://$BUCKET_NAME/test-object" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Delete object"
    else
        log_fail "Delete object"
        return 1
    fi
}

# ===== LARGE FILE TESTS =====

test_multipart_upload() {
    log_info "Testing: Multipart upload (10MB file)"
    generate_test_file $((10 * 1024 * 1024))
    if aws s3 cp "$TEST_FILE" "s3://$BUCKET_NAME/large-object" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Multipart upload"
    else
        log_fail "Multipart upload"
        return 1
    fi
}

test_multipart_download() {
    log_info "Testing: Multipart download"
    rm -f "$DOWNLOAD_FILE"
    if aws s3 cp "s3://$BUCKET_NAME/large-object" "$DOWNLOAD_FILE" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        if diff "$TEST_FILE" "$DOWNLOAD_FILE" > /dev/null 2>&1; then
            log_pass "Multipart download (content verified)"
        else
            log_fail "Multipart download (content mismatch)"
            return 1
        fi
    else
        log_fail "Multipart download"
        return 1
    fi
}

# ===== METADATA TESTS =====

test_put_object_with_metadata() {
    log_info "Testing: Put object with metadata"
    generate_test_file 512
    if aws s3 cp "$TEST_FILE" "s3://$BUCKET_NAME/metadata-object" \
        --metadata "author=test,version=1.0" \
        --content-type "application/json" \
        --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Put object with metadata"
    else
        log_fail "Put object with metadata"
        return 1
    fi
}

test_get_object_metadata() {
    log_info "Testing: Get object metadata"
    local metadata
    metadata=$(aws s3api head-object --bucket "$BUCKET_NAME" --key "metadata-object" --endpoint-url "$ENDPOINT_URL" 2>&1)
    if echo "$metadata" | grep -q "application/json"; then
        log_pass "Get object metadata (content-type)"
    else
        log_fail "Get object metadata"
        return 1
    fi
}

# ===== PRESIGNED URL TESTS =====

test_presign_get() {
    log_info "Testing: Presigned GET URL"
    local presigned_url
    presigned_url=$(aws s3 presign "s3://$BUCKET_NAME/test-object-copy" --endpoint-url "$ENDPOINT_URL" --expires-in 300 2>&1)
    if echo "$presigned_url" | grep -q "X-Amz-Signature"; then
        log_pass "Presigned GET URL generated"
    else
        log_fail "Presigned GET URL"
        return 1
    fi
}

# ===== VERSIONING TESTS =====

test_enable_versioning() {
    log_info "Testing: Enable bucket versioning"
    if aws s3api put-bucket-versioning \
        --bucket "$BUCKET_NAME" \
        --versioning-configuration Status=Enabled \
        --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Enable bucket versioning"
    else
        log_fail "Enable bucket versioning"
        return 1
    fi
}

test_get_versioning() {
    log_info "Testing: Get bucket versioning"
    local status
    status=$(aws s3api get-bucket-versioning --bucket "$BUCKET_NAME" --endpoint-url "$ENDPOINT_URL" 2>&1)
    if echo "$status" | grep -q "Enabled"; then
        log_pass "Get bucket versioning"
    else
        log_fail "Get bucket versioning"
        return 1
    fi
}

# ===== SYNC TESTS =====

test_sync() {
    log_info "Testing: Sync local to S3"
    local sync_dir="/tmp/sync-test-$$"
    mkdir -p "$sync_dir"
    for i in {1..5}; do
        dd if=/dev/urandom of="$sync_dir/file-$i.txt" bs=256 count=1 2>/dev/null
    done
    
    if aws s3 sync "$sync_dir" "s3://$BUCKET_NAME/sync-test/" --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Sync local to S3"
    else
        log_fail "Sync local to S3"
    fi
    
    rm -rf "$sync_dir"
}

# ===== BUCKET DELETION =====

test_delete_bucket() {
    log_info "Testing: Delete bucket (force)"
    if aws s3 rb "s3://$BUCKET_NAME" --force --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "Delete bucket"
    else
        log_fail "Delete bucket"
        return 1
    fi
}

# ===== MAIN =====

main() {
    echo "========================================"
    echo "AWS CLI Compatibility Tests"
    echo "Endpoint: $ENDPOINT_URL"
    echo "========================================"
    echo ""
    
    # Verify AWS CLI is installed
    if ! command -v aws &> /dev/null; then
        echo "AWS CLI is not installed. Please install it first."
        exit 1
    fi
    
    # Run tests
    test_create_bucket
    test_list_buckets
    test_head_bucket
    
    test_put_object
    test_get_object
    test_head_object
    test_list_objects
    test_copy_object
    
    test_put_object_with_metadata
    test_get_object_metadata
    
    test_presign_get
    
    test_enable_versioning
    test_get_versioning
    
    test_multipart_upload
    test_multipart_download
    
    test_sync
    
    test_delete_object
    test_delete_bucket
    
    echo ""
    echo "========================================"
    echo "Test Results"
    echo "========================================"
    echo -e "Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Failed: ${RED}$TESTS_FAILED${NC}"
    echo "========================================"
    
    if [ $TESTS_FAILED -gt 0 ]; then
        exit 1
    fi
}

main "$@"
