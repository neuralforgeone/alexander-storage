# Alexander Storage - SDK Compatibility Tests

Tests to verify S3 API compatibility with official AWS SDKs.

## Prerequisites

### AWS CLI
```bash
# Install AWS CLI
# macOS
brew install awscli

# Windows
choco install awscli

# Linux
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

Configure AWS CLI for Alexander:
```bash
aws configure set aws_access_key_id your-access-key
aws configure set aws_secret_access_key your-secret-key
aws configure set region us-east-1
```

### Python (boto3)
```bash
pip install boto3
```

## Running Tests

### AWS CLI Tests
```bash
# Set endpoint (optional, defaults to localhost:8080)
export ALEXANDER_URL=http://localhost:8080

# Run tests
chmod +x awscli_test.sh
./awscli_test.sh
```

### Boto3 Tests
```bash
# Set credentials (if different from defaults)
export ALEXANDER_URL=http://localhost:8080
export AWS_ACCESS_KEY_ID=your-access-key
export AWS_SECRET_ACCESS_KEY=your-secret-key

# Run tests
python boto3_test.py
```

## Test Coverage

### AWS CLI Tests (`awscli_test.sh`)
| Feature | Commands Tested |
|---------|-----------------|
| Bucket Operations | `mb`, `ls`, `rb`, `head-bucket` |
| Object Operations | `cp`, `rm`, `sync`, `head-object` |
| Large Files | Multipart upload/download (>5MB) |
| Metadata | `--metadata`, `--content-type` |
| Presigned URLs | `presign` |
| Versioning | `put-bucket-versioning`, `get-bucket-versioning` |

### Boto3 Tests (`boto3_test.py`)
| Test Class | Features Tested |
|------------|-----------------|
| `BucketTests` | create, list, head, delete buckets |
| `ObjectTests` | put, get, head, delete, copy, list objects |
| `MultipartTests` | initiate, upload parts, complete, abort, list parts |
| `VersioningTests` | enable, suspend, list versions, delete markers |
| `PresignedURLTests` | generate GET/PUT presigned URLs |
| `TransferTests` | upload_file, download_file, upload_fileobj |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ALEXANDER_URL` | `http://localhost:8080` | Alexander server endpoint |
| `AWS_ACCESS_KEY_ID` | `test-access-key` | Access key ID |
| `AWS_SECRET_ACCESS_KEY` | `test-secret-key` | Secret access key |
| `AWS_DEFAULT_REGION` | `us-east-1` | AWS region |

## Adding New Tests

### AWS CLI
Add new test functions in `awscli_test.sh`:
```bash
test_new_feature() {
    log_info "Testing: New feature"
    if aws s3api new-command --endpoint-url "$ENDPOINT_URL" 2>&1; then
        log_pass "New feature"
    else
        log_fail "New feature"
        return 1
    fi
}
```

### Boto3
Add new test class in `boto3_test.py`:
```python
class NewFeatureTests(unittest.TestCase):
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        # Cleanup
        pass
    
    def test_new_feature(self):
        """Test description."""
        response = self.s3.new_api_call(...)
        self.assertIn('ExpectedKey', response)
```

## Troubleshooting

### "Access Denied" Errors
- Verify credentials are correct
- Check that the access key exists in Alexander

### "Connection Refused" Errors
- Ensure Alexander server is running
- Check ALEXANDER_URL is correct

### Signature Mismatch
- Ensure system clock is synchronized
- Verify region setting matches server

### Large File Upload Failures
- Check available disk space
- Verify multipart upload configuration
