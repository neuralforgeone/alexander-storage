// S3 Client Helper for k6
// Implements AWS Signature V4 signing for S3-compatible requests

import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import http from 'k6/http';

const AWS_ALGORITHM = 'AWS4-HMAC-SHA256';
const AWS_SERVICE = 's3';
const AWS_REGION = 'us-east-1';

/**
 * Create HMAC-SHA256 signature
 */
function hmacSHA256(key, data) {
  return crypto.hmac('sha256', key, data, 'binary');
}

/**
 * Create SHA256 hash
 */
function sha256(data) {
  return crypto.sha256(data, 'hex');
}

/**
 * Get formatted date strings for AWS signing
 */
function getDateStrings() {
  const now = new Date();
  const dateStamp = now.toISOString().slice(0, 10).replace(/-/g, '');
  const amzDate = now.toISOString().replace(/[-:]/g, '').split('.')[0] + 'Z';
  return { dateStamp, amzDate };
}

/**
 * Create canonical request string
 */
function createCanonicalRequest(method, uri, queryString, headers, signedHeaders, payloadHash) {
  const canonicalHeaders = signedHeaders
    .split(';')
    .map(h => `${h}:${headers[h]}\n`)
    .join('');
  
  return [
    method,
    uri,
    queryString,
    canonicalHeaders,
    signedHeaders,
    payloadHash,
  ].join('\n');
}

/**
 * Create string to sign
 */
function createStringToSign(amzDate, credentialScope, canonicalRequestHash) {
  return [
    AWS_ALGORITHM,
    amzDate,
    credentialScope,
    canonicalRequestHash,
  ].join('\n');
}

/**
 * Create signing key
 */
function getSigningKey(secretKey, dateStamp, region, service) {
  const kDate = hmacSHA256('AWS4' + secretKey, dateStamp);
  const kRegion = hmacSHA256(kDate, region);
  const kService = hmacSHA256(kRegion, service);
  const kSigning = hmacSHA256(kService, 'aws4_request');
  return kSigning;
}

/**
 * Sign a request with AWS Signature V4
 */
export function signRequest(config, method, path, queryParams = {}, body = null, additionalHeaders = {}) {
  const { dateStamp, amzDate } = getDateStrings();
  const payloadHash = body ? sha256(body) : sha256('');
  
  const url = new URL(path, config.baseUrl);
  const host = url.host;
  const uri = url.pathname || '/';
  
  // Build query string
  const queryString = Object.keys(queryParams)
    .sort()
    .map(k => `${encodeURIComponent(k)}=${encodeURIComponent(queryParams[k])}`)
    .join('&');
  
  // Build headers
  const headers = {
    'host': host,
    'x-amz-date': amzDate,
    'x-amz-content-sha256': payloadHash,
    ...additionalHeaders,
  };
  
  // Sort header names for signing
  const signedHeaders = Object.keys(headers)
    .map(h => h.toLowerCase())
    .sort()
    .join(';');
  
  // Create canonical request
  const canonicalRequest = createCanonicalRequest(
    method,
    uri,
    queryString,
    Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v])),
    signedHeaders,
    payloadHash
  );
  
  // Create credential scope
  const credentialScope = `${dateStamp}/${AWS_REGION}/${AWS_SERVICE}/aws4_request`;
  
  // Create string to sign
  const stringToSign = createStringToSign(amzDate, credentialScope, sha256(canonicalRequest));
  
  // Create signature
  const signingKey = getSigningKey(config.secretAccessKey, dateStamp, AWS_REGION, AWS_SERVICE);
  const signature = crypto.hmac('sha256', signingKey, stringToSign, 'hex');
  
  // Create authorization header
  const authorization = `${AWS_ALGORITHM} Credential=${config.accessKeyId}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;
  
  return {
    url: queryString ? `${config.baseUrl}${uri}?${queryString}` : `${config.baseUrl}${uri}`,
    headers: {
      ...headers,
      'Authorization': authorization,
    },
  };
}

/**
 * S3 Client class for k6
 */
export class S3Client {
  constructor(config) {
    this.config = config;
  }
  
  /**
   * Create a bucket
   */
  createBucket(bucketName) {
    const { url, headers } = signRequest(this.config, 'PUT', `/${bucketName}`);
    return http.put(url, null, { headers, tags: { operation: 'create_bucket' } });
  }
  
  /**
   * Delete a bucket
   */
  deleteBucket(bucketName) {
    const { url, headers } = signRequest(this.config, 'DELETE', `/${bucketName}`);
    return http.del(url, null, { headers, tags: { operation: 'delete_bucket' } });
  }
  
  /**
   * List buckets
   */
  listBuckets() {
    const { url, headers } = signRequest(this.config, 'GET', '/');
    return http.get(url, { headers, tags: { operation: 'list_buckets' } });
  }
  
  /**
   * Put an object
   */
  putObject(bucketName, key, body, contentType = 'application/octet-stream') {
    const additionalHeaders = {
      'content-type': contentType,
      'content-length': body.length.toString(),
    };
    const { url, headers } = signRequest(
      this.config,
      'PUT',
      `/${bucketName}/${key}`,
      {},
      body,
      additionalHeaders
    );
    return http.put(url, body, { headers, tags: { operation: 'put_object' } });
  }
  
  /**
   * Get an object
   */
  getObject(bucketName, key) {
    const { url, headers } = signRequest(this.config, 'GET', `/${bucketName}/${key}`);
    return http.get(url, { headers, tags: { operation: 'get_object' } });
  }
  
  /**
   * Delete an object
   */
  deleteObject(bucketName, key) {
    const { url, headers } = signRequest(this.config, 'DELETE', `/${bucketName}/${key}`);
    return http.del(url, null, { headers, tags: { operation: 'delete_object' } });
  }
  
  /**
   * Head an object
   */
  headObject(bucketName, key) {
    const { url, headers } = signRequest(this.config, 'HEAD', `/${bucketName}/${key}`);
    return http.head(url, { headers, tags: { operation: 'head_object' } });
  }
  
  /**
   * List objects in a bucket
   */
  listObjects(bucketName, prefix = '', maxKeys = 1000) {
    const queryParams = {
      'list-type': '2',
      'max-keys': maxKeys.toString(),
    };
    if (prefix) {
      queryParams['prefix'] = prefix;
    }
    const { url, headers } = signRequest(this.config, 'GET', `/${bucketName}`, queryParams);
    return http.get(url, { headers, tags: { operation: 'list_objects' } });
  }
  
  /**
   * Copy an object
   */
  copyObject(sourceBucket, sourceKey, destBucket, destKey) {
    const additionalHeaders = {
      'x-amz-copy-source': `/${sourceBucket}/${sourceKey}`,
    };
    const { url, headers } = signRequest(
      this.config,
      'PUT',
      `/${destBucket}/${destKey}`,
      {},
      null,
      additionalHeaders
    );
    return http.put(url, null, { headers, tags: { operation: 'copy_object' } });
  }
}

/**
 * Generate random data of specified size
 */
export function generateRandomData(sizeBytes) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let result = '';
  for (let i = 0; i < sizeBytes; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

/**
 * Generate a unique key name
 */
export function generateKey(prefix = 'test') {
  return `${prefix}/${Date.now()}-${Math.random().toString(36).substring(7)}`;
}
