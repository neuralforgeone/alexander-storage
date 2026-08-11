// Package handler provides HTTP handlers for Alexander Storage API.
package handler

import (
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/auth"
	"github.com/prn-tf/alexander-storage/internal/metrics"
	"github.com/prn-tf/alexander-storage/internal/middleware"
)

// Router handles HTTP routing for the S3-compatible API.
type Router struct {
	bucketHandler     *BucketHandler
	objectHandler     *ObjectHandler
	multipartHandler  *MultipartHandler
	healthChecker     *HealthChecker
	authMiddleware    func(http.Handler) http.Handler
	rateLimiter       *middleware.RateLimiter
	tracing           *middleware.Tracing
	metricsMiddleware *middleware.MetricsMiddleware
	metrics           *metrics.Metrics
	logger            zerolog.Logger
}

// RouterConfig contains configuration for the router.
type RouterConfig struct {
	BucketHandler    *BucketHandler
	ObjectHandler    *ObjectHandler
	MultipartHandler *MultipartHandler
	HealthChecker    *HealthChecker
	AuthMiddleware   func(http.Handler) http.Handler
	RateLimiter      *middleware.RateLimiter
	Tracing          *middleware.Tracing
	Metrics          *metrics.Metrics
	Logger           zerolog.Logger
}

// NewRouter creates a new Router.
func NewRouter(config RouterConfig) *Router {
	var metricsMiddleware *middleware.MetricsMiddleware
	if config.Metrics != nil {
		metricsMiddleware = middleware.NewMetricsMiddleware(config.Metrics)
	}

	return &Router{
		bucketHandler:     config.BucketHandler,
		objectHandler:     config.ObjectHandler,
		multipartHandler:  config.MultipartHandler,
		healthChecker:     config.HealthChecker,
		authMiddleware:    config.AuthMiddleware,
		rateLimiter:       config.RateLimiter,
		tracing:           config.Tracing,
		metricsMiddleware: metricsMiddleware,
		metrics:           config.Metrics,
		logger:            config.Logger.With().Str("component", "router").Logger(),
	}
}

// Handler returns the main HTTP handler.
func (rt *Router) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoints (no auth, no rate limiting)
	if rt.healthChecker != nil {
		mux.HandleFunc("/health", rt.healthChecker.HandleHealth)
		mux.HandleFunc("/healthz", rt.healthChecker.HandleLiveness)
		mux.HandleFunc("/readyz", rt.healthChecker.HandleReadiness)
	} else {
		mux.HandleFunc("/health", rt.handleHealth)
	}

	// Main S3 API handler
	mux.HandleFunc("/", rt.handleS3Request)

	// Build middleware chain (innermost to outermost)
	var handler http.Handler = mux

	// Auth middleware (innermost - after tracing, before rate limiting)
	handler = rt.authMiddleware(handler)

	// Rate limiting middleware
	if rt.rateLimiter != nil {
		handler = rt.rateLimiter.Middleware(handler)
	}

	// Metrics middleware (track in-flight requests)
	if rt.metricsMiddleware != nil {
		handler = rt.metricsMiddleware.Middleware(handler)
	}

	// Tracing middleware (outermost - first to execute)
	if rt.tracing != nil {
		handler = rt.tracing.Middleware(handler)
	}

	return handler
}

// handleHealth handles health check requests.
func (rt *Router) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

// handleS3Request routes S3 API requests to appropriate handlers.
func (rt *Router) handleS3Request(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	query := r.URL.Query()

	// Root path - list all buckets
	if path == "/" {
		if r.Method == http.MethodGet {
			rt.bucketHandler.ListBuckets(w, r)
			return
		}
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
		return
	}

	// Extract bucket name and key from path
	// Path format: /{bucket} or /{bucket}/{key...}
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	bucketName := parts[0]
	var objectKey string
	if len(parts) > 1 {
		objectKey = parts[1]
	}

	// Object operations (when key is present)
	if objectKey != "" {
		rt.handleObjectRequest(w, r, bucketName, objectKey)
		return
	}

	// Bucket operations
	rt.handleBucketRequest(w, r, bucketName, query)
}

// handleBucketRequest routes bucket-level requests.
func (rt *Router) handleBucketRequest(w http.ResponseWriter, r *http.Request, bucketName string, query map[string][]string) {
	// Check for sub-resource operations
	if _, ok := query["versioning"]; ok {
		switch r.Method {
		case http.MethodGet:
			rt.bucketHandler.GetBucketVersioning(w, r)
		case http.MethodPut:
			rt.bucketHandler.PutBucketVersioning(w, r)
		default:
			writeError(w, S3Error{
				Code:           "MethodNotAllowed",
				Message:        "The specified method is not allowed against this resource.",
				HTTPStatusCode: http.StatusMethodNotAllowed,
			})
		}
		return
	}

	// Check for versions sub-resource (ListObjectVersions)
	if _, ok := query["versions"]; ok {
		if r.Method == http.MethodGet {
			rt.objectHandler.ListObjectVersions(w, r, bucketName)
			return
		}
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
		return
	}

	// Check for uploads sub-resource (ListMultipartUploads)
	if _, ok := query["uploads"]; ok {
		if r.Method == http.MethodGet {
			rt.multipartHandler.ListMultipartUploads(w, r, bucketName)
			return
		}
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
		return
	}

	// Multi-object delete: POST /{bucket}?delete=
	if _, ok := query["delete"]; ok {
		if r.Method == http.MethodPost {
			rt.objectHandler.DeleteObjects(w, r, bucketName)
			return
		}
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
		return
	}

	// TODO: Add more sub-resources (lifecycle, policy, acl, etc.)

	// Basic bucket operations
	switch r.Method {
	case http.MethodHead:
		rt.bucketHandler.HeadBucket(w, r)
	case http.MethodGet:
		// GET /{bucket} without sub-resource = ListObjects
		// For now, we'll treat it as HeadBucket since ListObjects is in Phase 4
		rt.handleListObjects(w, r, bucketName)
	case http.MethodPut:
		rt.bucketHandler.CreateBucket(w, r)
	case http.MethodDelete:
		rt.bucketHandler.DeleteBucket(w, r)
	default:
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
	}
}

// handleObjectRequest routes object-level requests.
func (rt *Router) handleObjectRequest(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	query := r.URL.Query()

	// Check for multipart upload operations
	uploadID := query.Get("uploadId")
	_, hasUploads := query["uploads"]

	// InitiateMultipartUpload: POST /{bucket}/{key}?uploads
	if hasUploads && r.Method == http.MethodPost {
		rt.multipartHandler.InitiateMultipartUpload(w, r, bucketName, objectKey)
		return
	}

	// Operations that require uploadId
	if uploadID != "" {
		switch r.Method {
		case http.MethodPut:
			// UploadPart: PUT /{bucket}/{key}?partNumber=N&uploadId=X
			rt.multipartHandler.UploadPart(w, r, bucketName, objectKey)
			return
		case http.MethodPost:
			// CompleteMultipartUpload: POST /{bucket}/{key}?uploadId=X
			rt.multipartHandler.CompleteMultipartUpload(w, r, bucketName, objectKey)
			return
		case http.MethodDelete:
			// AbortMultipartUpload: DELETE /{bucket}/{key}?uploadId=X
			rt.multipartHandler.AbortMultipartUpload(w, r, bucketName, objectKey)
			return
		case http.MethodGet:
			// ListParts: GET /{bucket}/{key}?uploadId=X
			rt.multipartHandler.ListParts(w, r, bucketName, objectKey)
			return
		}
	}

	// Standard object operations
	switch r.Method {
	case http.MethodGet:
		rt.objectHandler.GetObject(w, r, bucketName, objectKey)
	case http.MethodHead:
		rt.objectHandler.HeadObject(w, r, bucketName, objectKey)
	case http.MethodPut:
		// Check for copy operation (x-amz-copy-source header)
		if r.Header.Get("x-amz-copy-source") != "" {
			rt.objectHandler.CopyObject(w, r, bucketName, objectKey)
			return
		}
		rt.objectHandler.PutObject(w, r, bucketName, objectKey)
	case http.MethodDelete:
		rt.objectHandler.DeleteObject(w, r, bucketName, objectKey)
	default:
		writeError(w, S3Error{
			Code:           "MethodNotAllowed",
			Message:        "The specified method is not allowed against this resource.",
			HTTPStatusCode: http.StatusMethodNotAllowed,
		})
	}
}

// handleListObjects handles ListObjects requests.
func (rt *Router) handleListObjects(w http.ResponseWriter, r *http.Request, bucketName string) {
	query := r.URL.Query()

	// Check for list-type=2 (ListObjectsV2)
	if query.Get("list-type") == "2" {
		rt.objectHandler.ListObjectsV2(w, r, bucketName)
		return
	}

	// ListObjectsV1
	rt.objectHandler.ListObjects(w, r, bucketName)
}

// CreateAuthMiddleware creates an authentication middleware using the provided store.
func CreateAuthMiddleware(store auth.AccessKeyStore, config auth.Config) func(http.Handler) http.Handler {
	return auth.Middleware(store, config)
}
