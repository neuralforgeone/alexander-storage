#!/usr/bin/env python3
"""
Boto3 (AWS SDK for Python) Compatibility Tests for Alexander Storage

This script tests S3 API compatibility using the official boto3 SDK.
"""

import os
import sys
import time
import hashlib
import tempfile
import unittest
from io import BytesIO
from typing import Optional

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError


# Configuration
ENDPOINT_URL = os.environ.get('ALEXANDER_URL', 'http://localhost:8080')
ACCESS_KEY = os.environ.get('AWS_ACCESS_KEY_ID', 'test-access-key')
SECRET_KEY = os.environ.get('AWS_SECRET_ACCESS_KEY', 'test-secret-key')
REGION = os.environ.get('AWS_DEFAULT_REGION', 'us-east-1')


def get_s3_client():
    """Create an S3 client configured for Alexander Storage."""
    return boto3.client(
        's3',
        endpoint_url=ENDPOINT_URL,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        region_name=REGION,
        config=Config(
            signature_version='s3v4',
            s3={'addressing_style': 'path'}
        )
    )


def get_s3_resource():
    """Create an S3 resource configured for Alexander Storage."""
    return boto3.resource(
        's3',
        endpoint_url=ENDPOINT_URL,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        region_name=REGION,
        config=Config(
            signature_version='s3v4',
            s3={'addressing_style': 'path'}
        )
    )


class BucketTests(unittest.TestCase):
    """Tests for bucket operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'boto3-test-{int(time.time())}'
    
    def tearDown(self):
        try:
            # Delete all objects
            s3_resource = get_s3_resource()
            bucket = s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            bucket.object_versions.all().delete()
            # Delete bucket
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_create_bucket(self):
        """Test bucket creation."""
        response = self.s3.create_bucket(Bucket=self.bucket_name)
        self.assertIn('Location', response)
    
    def test_list_buckets(self):
        """Test listing buckets."""
        self.s3.create_bucket(Bucket=self.bucket_name)
        response = self.s3.list_buckets()
        
        bucket_names = [b['Name'] for b in response['Buckets']]
        self.assertIn(self.bucket_name, bucket_names)
    
    def test_head_bucket(self):
        """Test head bucket."""
        self.s3.create_bucket(Bucket=self.bucket_name)
        # Should not raise exception
        self.s3.head_bucket(Bucket=self.bucket_name)
    
    def test_head_bucket_not_found(self):
        """Test head bucket for non-existent bucket."""
        with self.assertRaises(ClientError) as ctx:
            self.s3.head_bucket(Bucket='non-existent-bucket-12345')
        self.assertEqual(ctx.exception.response['Error']['Code'], '404')
    
    def test_delete_bucket(self):
        """Test bucket deletion."""
        self.s3.create_bucket(Bucket=self.bucket_name)
        self.s3.delete_bucket(Bucket=self.bucket_name)
        
        # Should not appear in list
        response = self.s3.list_buckets()
        bucket_names = [b['Name'] for b in response['Buckets']]
        self.assertNotIn(self.bucket_name, bucket_names)


class ObjectTests(unittest.TestCase):
    """Tests for object operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'boto3-obj-test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        try:
            s3_resource = get_s3_resource()
            bucket = s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            bucket.object_versions.all().delete()
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_put_object(self):
        """Test putting an object."""
        data = b'Hello, Alexander!'
        response = self.s3.put_object(
            Bucket=self.bucket_name,
            Key='test-key',
            Body=data
        )
        self.assertIn('ETag', response)
    
    def test_get_object(self):
        """Test getting an object."""
        data = b'Hello, Alexander!'
        self.s3.put_object(Bucket=self.bucket_name, Key='test-key', Body=data)
        
        response = self.s3.get_object(Bucket=self.bucket_name, Key='test-key')
        body = response['Body'].read()
        self.assertEqual(body, data)
    
    def test_head_object(self):
        """Test head object."""
        data = b'Test data'
        self.s3.put_object(Bucket=self.bucket_name, Key='test-key', Body=data)
        
        response = self.s3.head_object(Bucket=self.bucket_name, Key='test-key')
        self.assertEqual(response['ContentLength'], len(data))
    
    def test_delete_object(self):
        """Test deleting an object."""
        self.s3.put_object(Bucket=self.bucket_name, Key='test-key', Body=b'data')
        self.s3.delete_object(Bucket=self.bucket_name, Key='test-key')
        
        with self.assertRaises(ClientError) as ctx:
            self.s3.head_object(Bucket=self.bucket_name, Key='test-key')
        self.assertEqual(ctx.exception.response['Error']['Code'], '404')
    
    def test_copy_object(self):
        """Test copying an object."""
        data = b'Original data'
        self.s3.put_object(Bucket=self.bucket_name, Key='source-key', Body=data)
        
        self.s3.copy_object(
            Bucket=self.bucket_name,
            Key='dest-key',
            CopySource={'Bucket': self.bucket_name, 'Key': 'source-key'}
        )
        
        response = self.s3.get_object(Bucket=self.bucket_name, Key='dest-key')
        self.assertEqual(response['Body'].read(), data)
    
    def test_list_objects(self):
        """Test listing objects."""
        for i in range(5):
            self.s3.put_object(
                Bucket=self.bucket_name,
                Key=f'prefix/object-{i}',
                Body=f'data-{i}'.encode()
            )
        
        response = self.s3.list_objects_v2(
            Bucket=self.bucket_name,
            Prefix='prefix/'
        )
        
        self.assertEqual(response['KeyCount'], 5)
    
    def test_object_metadata(self):
        """Test object with custom metadata."""
        metadata = {'author': 'test', 'version': '1.0'}
        self.s3.put_object(
            Bucket=self.bucket_name,
            Key='metadata-test',
            Body=b'data',
            Metadata=metadata,
            ContentType='application/json'
        )
        
        response = self.s3.head_object(Bucket=self.bucket_name, Key='metadata-test')
        self.assertEqual(response['ContentType'], 'application/json')
        self.assertEqual(response['Metadata'], metadata)
    
    def test_object_not_found(self):
        """Test getting non-existent object."""
        with self.assertRaises(ClientError) as ctx:
            self.s3.get_object(Bucket=self.bucket_name, Key='non-existent')
        self.assertEqual(ctx.exception.response['Error']['Code'], 'NoSuchKey')


class MultipartTests(unittest.TestCase):
    """Tests for multipart upload operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'boto3-mp-test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        try:
            s3_resource = get_s3_resource()
            bucket = s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            bucket.object_versions.all().delete()
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_multipart_upload(self):
        """Test multipart upload."""
        key = 'multipart-test'
        
        # Initiate multipart upload
        response = self.s3.create_multipart_upload(
            Bucket=self.bucket_name,
            Key=key
        )
        upload_id = response['UploadId']
        
        # Upload parts (minimum 5MB per part, except last)
        part_size = 5 * 1024 * 1024  # 5MB
        parts = []
        
        for i in range(3):
            data = os.urandom(part_size)
            response = self.s3.upload_part(
                Bucket=self.bucket_name,
                Key=key,
                UploadId=upload_id,
                PartNumber=i + 1,
                Body=data
            )
            parts.append({
                'PartNumber': i + 1,
                'ETag': response['ETag']
            })
        
        # Complete multipart upload
        response = self.s3.complete_multipart_upload(
            Bucket=self.bucket_name,
            Key=key,
            UploadId=upload_id,
            MultipartUpload={'Parts': parts}
        )
        
        self.assertIn('ETag', response)
        
        # Verify object exists and has correct size
        head = self.s3.head_object(Bucket=self.bucket_name, Key=key)
        self.assertEqual(head['ContentLength'], part_size * 3)
    
    def test_abort_multipart_upload(self):
        """Test aborting multipart upload."""
        key = 'abort-test'
        
        # Initiate multipart upload
        response = self.s3.create_multipart_upload(
            Bucket=self.bucket_name,
            Key=key
        )
        upload_id = response['UploadId']
        
        # Abort
        self.s3.abort_multipart_upload(
            Bucket=self.bucket_name,
            Key=key,
            UploadId=upload_id
        )
        
        # Verify upload is aborted (list uploads should not contain it)
        response = self.s3.list_multipart_uploads(Bucket=self.bucket_name)
        upload_ids = [u['UploadId'] for u in response.get('Uploads', [])]
        self.assertNotIn(upload_id, upload_ids)
    
    def test_list_parts(self):
        """Test listing parts of multipart upload."""
        key = 'list-parts-test'
        
        # Initiate
        response = self.s3.create_multipart_upload(
            Bucket=self.bucket_name,
            Key=key
        )
        upload_id = response['UploadId']
        
        # Upload some parts
        for i in range(3):
            self.s3.upload_part(
                Bucket=self.bucket_name,
                Key=key,
                UploadId=upload_id,
                PartNumber=i + 1,
                Body=os.urandom(5 * 1024 * 1024)
            )
        
        # List parts
        response = self.s3.list_parts(
            Bucket=self.bucket_name,
            Key=key,
            UploadId=upload_id
        )
        
        self.assertEqual(len(response['Parts']), 3)
        
        # Cleanup
        self.s3.abort_multipart_upload(
            Bucket=self.bucket_name,
            Key=key,
            UploadId=upload_id
        )


class VersioningTests(unittest.TestCase):
    """Tests for versioning operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'boto3-ver-test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        try:
            s3_resource = get_s3_resource()
            bucket = s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            bucket.object_versions.all().delete()
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_enable_versioning(self):
        """Test enabling bucket versioning."""
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Enabled'}
        )
        
        response = self.s3.get_bucket_versioning(Bucket=self.bucket_name)
        self.assertEqual(response['Status'], 'Enabled')
    
    def test_suspend_versioning(self):
        """Test suspending bucket versioning."""
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Enabled'}
        )
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Suspended'}
        )
        
        response = self.s3.get_bucket_versioning(Bucket=self.bucket_name)
        self.assertEqual(response['Status'], 'Suspended')
    
    def test_object_versions(self):
        """Test object versioning."""
        # Enable versioning
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Enabled'}
        )
        
        # Create multiple versions
        key = 'versioned-object'
        versions = []
        for i in range(3):
            response = self.s3.put_object(
                Bucket=self.bucket_name,
                Key=key,
                Body=f'version-{i}'.encode()
            )
            versions.append(response['VersionId'])
        
        # List versions
        response = self.s3.list_object_versions(
            Bucket=self.bucket_name,
            Prefix=key
        )
        
        self.assertEqual(len(response['Versions']), 3)
    
    def test_get_specific_version(self):
        """Test getting a specific object version."""
        # Enable versioning
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Enabled'}
        )
        
        # Create versions
        key = 'version-test'
        v1 = self.s3.put_object(
            Bucket=self.bucket_name,
            Key=key,
            Body=b'version-1'
        )['VersionId']
        
        self.s3.put_object(
            Bucket=self.bucket_name,
            Key=key,
            Body=b'version-2'
        )
        
        # Get specific version
        response = self.s3.get_object(
            Bucket=self.bucket_name,
            Key=key,
            VersionId=v1
        )
        
        self.assertEqual(response['Body'].read(), b'version-1')
    
    def test_delete_marker(self):
        """Test delete markers in versioned bucket."""
        # Enable versioning
        self.s3.put_bucket_versioning(
            Bucket=self.bucket_name,
            VersioningConfiguration={'Status': 'Enabled'}
        )
        
        # Create object
        key = 'delete-marker-test'
        self.s3.put_object(Bucket=self.bucket_name, Key=key, Body=b'data')
        
        # Delete object (creates delete marker)
        delete_response = self.s3.delete_object(
            Bucket=self.bucket_name,
            Key=key
        )
        
        self.assertTrue(delete_response.get('DeleteMarker', False))
        
        # List versions should show delete marker
        response = self.s3.list_object_versions(
            Bucket=self.bucket_name,
            Prefix=key
        )
        
        self.assertIn('DeleteMarkers', response)


class PresignedURLTests(unittest.TestCase):
    """Tests for presigned URL operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.bucket_name = f'boto3-presign-test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        try:
            s3_resource = get_s3_resource()
            bucket = s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_generate_presigned_get_url(self):
        """Test generating presigned GET URL."""
        self.s3.put_object(
            Bucket=self.bucket_name,
            Key='presign-test',
            Body=b'test data'
        )
        
        url = self.s3.generate_presigned_url(
            'get_object',
            Params={'Bucket': self.bucket_name, 'Key': 'presign-test'},
            ExpiresIn=300
        )
        
        self.assertIn('X-Amz-Signature', url)
        self.assertIn('X-Amz-Expires', url)
    
    def test_generate_presigned_put_url(self):
        """Test generating presigned PUT URL."""
        url = self.s3.generate_presigned_url(
            'put_object',
            Params={'Bucket': self.bucket_name, 'Key': 'presign-upload'},
            ExpiresIn=300
        )
        
        self.assertIn('X-Amz-Signature', url)


class TransferTests(unittest.TestCase):
    """Tests for high-level transfer operations."""
    
    def setUp(self):
        self.s3 = get_s3_client()
        self.s3_resource = get_s3_resource()
        self.bucket_name = f'boto3-transfer-test-{int(time.time())}'
        self.s3.create_bucket(Bucket=self.bucket_name)
    
    def tearDown(self):
        try:
            bucket = self.s3_resource.Bucket(self.bucket_name)
            bucket.objects.all().delete()
            self.s3.delete_bucket(Bucket=self.bucket_name)
        except ClientError:
            pass
    
    def test_upload_file(self):
        """Test file upload using transfer manager."""
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(os.urandom(1024 * 1024))  # 1MB
            temp_path = f.name
        
        try:
            self.s3.upload_file(temp_path, self.bucket_name, 'upload-test')
            
            # Verify
            response = self.s3.head_object(Bucket=self.bucket_name, Key='upload-test')
            self.assertEqual(response['ContentLength'], 1024 * 1024)
        finally:
            os.unlink(temp_path)
    
    def test_download_file(self):
        """Test file download using transfer manager."""
        data = os.urandom(1024 * 1024)
        self.s3.put_object(Bucket=self.bucket_name, Key='download-test', Body=data)
        
        with tempfile.NamedTemporaryFile(delete=False) as f:
            temp_path = f.name
        
        try:
            self.s3.download_file(self.bucket_name, 'download-test', temp_path)
            
            with open(temp_path, 'rb') as f:
                downloaded = f.read()
            
            self.assertEqual(downloaded, data)
        finally:
            os.unlink(temp_path)
    
    def test_upload_fileobj(self):
        """Test file-like object upload."""
        data = b'File object data'
        fileobj = BytesIO(data)
        
        self.s3.upload_fileobj(fileobj, self.bucket_name, 'fileobj-test')
        
        response = self.s3.get_object(Bucket=self.bucket_name, Key='fileobj-test')
        self.assertEqual(response['Body'].read(), data)
    
    def test_download_fileobj(self):
        """Test file-like object download."""
        data = b'Download test data'
        self.s3.put_object(Bucket=self.bucket_name, Key='fileobj-download', Body=data)
        
        fileobj = BytesIO()
        self.s3.download_fileobj(self.bucket_name, 'fileobj-download', fileobj)
        
        fileobj.seek(0)
        self.assertEqual(fileobj.read(), data)


def main():
    """Run all tests."""
    # Print configuration
    print(f"Testing against: {ENDPOINT_URL}")
    print(f"Access Key: {ACCESS_KEY[:4]}...{ACCESS_KEY[-4:]}")
    print("-" * 60)
    
    # Run tests
    loader = unittest.TestLoader()
    suite = unittest.TestSuite()
    
    # Add test classes
    suite.addTests(loader.loadTestsFromTestCase(BucketTests))
    suite.addTests(loader.loadTestsFromTestCase(ObjectTests))
    suite.addTests(loader.loadTestsFromTestCase(MultipartTests))
    suite.addTests(loader.loadTestsFromTestCase(VersioningTests))
    suite.addTests(loader.loadTestsFromTestCase(PresignedURLTests))
    suite.addTests(loader.loadTestsFromTestCase(TransferTests))
    
    # Run
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    
    # Exit with appropriate code
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == '__main__':
    main()
