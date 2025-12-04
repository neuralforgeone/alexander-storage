// Package handler provides HTTP handlers for Alexander Storage API.
package handler

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/auth"
	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/service"
)

// BucketHandler handles bucket-related HTTP requests.
type BucketHandler struct {
	bucketService *service.BucketService
	logger        zerolog.Logger
}

// NewBucketHandler creates a new BucketHandler.
func NewBucketHandler(bucketService *service.BucketService, logger zerolog.Logger) *BucketHandler {
	return &BucketHandler{
		bucketService: bucketService,
		logger:        logger.With().Str("handler", "bucket").Logger(),
	}
}

// =============================================================================
// XML Request/Response Types
// =============================================================================

// ListAllMyBucketsResult is the response for ListBuckets.
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Xmlns   string   `xml:"xmlns,attr"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

// Buckets is a container for bucket list.
type Buckets struct {
	Bucket []BucketInfo `xml:"Bucket"`
}

// BucketInfo represents bucket info in list response.
type BucketInfo struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// CreateBucketConfiguration is the request body for CreateBucket.
type CreateBucketConfiguration struct {
	XMLName            xml.Name `xml:"CreateBucketConfiguration"`
	LocationConstraint string   `xml:"LocationConstraint"`
}

// VersioningConfiguration is the request/response for bucket versioning.
type VersioningConfiguration struct {
	XMLName   xml.Name `xml:"VersioningConfiguration"`
	Xmlns     string   `xml:"xmlns,attr,omitempty"`
	Status    string   `xml:"Status,omitempty"`
	MFADelete string   `xml:"MfaDelete,omitempty"`
}

// =============================================================================
// Handler Methods
// =============================================================================

// CreateBucket handles PUT /{bucket} requests.
func (h *BucketHandler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Extract bucket name from path
	bucketName := extractBucketName(r)
	if bucketName == "" {
		writeError(w, ErrInvalidBucketName)
		return
	}

	// Parse optional location constraint from body
	var region string
	if r.ContentLength > 0 {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*10)) // 10KB limit
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to read request body")
			writeError(w, ErrInternalError)
			return
		}
		defer r.Body.Close()

		if len(body) > 0 {
			var config CreateBucketConfiguration
			if err := xml.Unmarshal(body, &config); err != nil {
				writeError(w, ErrMalformedXML)
				return
			}
			region = config.LocationConstraint
		}
	}

	// Create bucket
	output, err := h.bucketService.CreateBucket(ctx, service.CreateBucketInput{
		OwnerID: userCtx.UserID,
		Name:    bucketName,
		Region:  region,
	})

	if err != nil {
		h.handleError(w, err, bucketName)
		return
	}

	// Success - return 200 with Location header
	w.Header().Set("Location", "/"+output.Bucket.Name)
	w.WriteHeader(http.StatusOK)
}

// DeleteBucket handles DELETE /{bucket} requests.
func (h *BucketHandler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Extract bucket name from path
	bucketName := extractBucketName(r)
	if bucketName == "" {
		writeError(w, ErrInvalidBucketName)
		return
	}

	// Delete bucket
	err := h.bucketService.DeleteBucket(ctx, service.DeleteBucketInput{
		Name:    bucketName,
		OwnerID: userCtx.UserID,
	})

	if err != nil {
		h.handleError(w, err, bucketName)
		return
	}

	// Success - return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// ListBuckets handles GET / requests (list all buckets).
func (h *BucketHandler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// List buckets
	output, err := h.bucketService.ListBuckets(ctx, service.ListBucketsInput{
		OwnerID: userCtx.UserID,
	})

	if err != nil {
		h.handleError(w, err, "")
		return
	}

	// Build response
	buckets := make([]BucketInfo, len(output.Buckets))
	for i, b := range output.Buckets {
		buckets[i] = BucketInfo{
			Name:         b.Name,
			CreationDate: formatS3Time(b.CreatedAt),
		}
	}

	response := ListAllMyBucketsResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: Owner{
			ID:          userCtx.Username,
			DisplayName: userCtx.Username,
		},
		Buckets: Buckets{
			Bucket: buckets,
		},
	}

	writeXML(w, http.StatusOK, response)
}

// HeadBucket handles HEAD /{bucket} requests.
func (h *BucketHandler) HeadBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Extract bucket name from path
	bucketName := extractBucketName(r)
	if bucketName == "" {
		writeError(w, ErrInvalidBucketName)
		return
	}

	// Check bucket
	output, err := h.bucketService.HeadBucket(ctx, service.HeadBucketInput{
		Name:    bucketName,
		OwnerID: userCtx.UserID,
	})

	if err != nil {
		h.handleError(w, err, bucketName)
		return
	}

	if !output.Exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Success - return 200 with headers
	w.Header().Set("x-amz-bucket-region", output.Region)
	w.WriteHeader(http.StatusOK)
}

// GetBucketVersioning handles GET /{bucket}?versioning requests.
func (h *BucketHandler) GetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Extract bucket name from path
	bucketName := extractBucketName(r)
	if bucketName == "" {
		writeError(w, ErrInvalidBucketName)
		return
	}

	// Get versioning status
	output, err := h.bucketService.GetBucketVersioning(ctx, service.GetBucketVersioningInput{
		Name:    bucketName,
		OwnerID: userCtx.UserID,
	})

	if err != nil {
		h.handleError(w, err, bucketName)
		return
	}

	// Build response
	response := VersioningConfiguration{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
	}

	// Only include Status if versioning was ever enabled
	if output.Status == domain.VersioningEnabled {
		response.Status = "Enabled"
	} else if output.Status == domain.VersioningSuspended {
		response.Status = "Suspended"
	}
	// If Disabled, Status element is omitted (empty response body with just the root element)

	writeXML(w, http.StatusOK, response)
}

// PutBucketVersioning handles PUT /{bucket}?versioning requests.
func (h *BucketHandler) PutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Extract bucket name from path
	bucketName := extractBucketName(r)
	if bucketName == "" {
		writeError(w, ErrInvalidBucketName)
		return
	}

	// Parse request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*10)) // 10KB limit
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to read request body")
		writeError(w, ErrInternalError)
		return
	}
	defer r.Body.Close()

	var config VersioningConfiguration
	if err := xml.Unmarshal(body, &config); err != nil {
		writeError(w, ErrMalformedXML)
		return
	}

	// Convert to domain status
	var status domain.VersioningStatus
	switch config.Status {
	case "Enabled":
		status = domain.VersioningEnabled
	case "Suspended":
		status = domain.VersioningSuspended
	default:
		writeError(w, ErrIllegalVersioningConfigurationException)
		return
	}

	// Update versioning
	err = h.bucketService.PutBucketVersioning(ctx, service.PutBucketVersioningInput{
		Name:    bucketName,
		OwnerID: userCtx.UserID,
		Status:  status,
	})

	if err != nil {
		h.handleError(w, err, bucketName)
		return
	}

	// Success - return 200
	w.WriteHeader(http.StatusOK)
}

// =============================================================================
// Helper Methods
// =============================================================================

// extractBucketName extracts the bucket name from the request path.
// Supports both path-style (/{bucket}) and virtual-hosted style (bucket.host.com).
func extractBucketName(r *http.Request) string {
	// For now, we only support path-style addressing
	// Path format: /{bucket} or /{bucket}/{key}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// handleError maps service errors to S3 error responses.
func (h *BucketHandler) handleError(w http.ResponseWriter, err error, resource string) {
	s3Err := ErrInternalError
	s3Err.Resource = resource

	switch {
	case errors.Is(err, domain.ErrBucketNotFound):
		s3Err = ErrNoSuchBucket
	case errors.Is(err, domain.ErrBucketAlreadyExists):
		s3Err = ErrBucketAlreadyExists
	case errors.Is(err, domain.ErrBucketNotEmpty):
		s3Err = ErrBucketNotEmpty
	case errors.Is(err, domain.ErrBucketNameLength),
		errors.Is(err, domain.ErrBucketNameFormat),
		errors.Is(err, domain.ErrBucketNameIPFormat):
		s3Err = ErrInvalidBucketName
		s3Err.Message = err.Error()
	case errors.Is(err, service.ErrBucketAccessDenied):
		s3Err = ErrAccessDenied
	case errors.Is(err, service.ErrInvalidVersioningStatus):
		s3Err = ErrIllegalVersioningConfigurationException
	default:
		h.logger.Error().Err(err).Str("resource", resource).Msg("unhandled error")
	}

	s3Err.Resource = resource
	writeError(w, s3Err)
}
