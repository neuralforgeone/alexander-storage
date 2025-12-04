// Package handler provides HTTP handlers for Alexander Storage API.
package handler

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/auth"
	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/service"
)

// ObjectHandler handles object-related HTTP requests.
type ObjectHandler struct {
	objectService *service.ObjectService
	logger        zerolog.Logger
}

// NewObjectHandler creates a new ObjectHandler.
func NewObjectHandler(objectService *service.ObjectService, logger zerolog.Logger) *ObjectHandler {
	return &ObjectHandler{
		objectService: objectService,
		logger:        logger.With().Str("handler", "object").Logger(),
	}
}

// =============================================================================
// XML Types
// =============================================================================

// ListBucketResult is the response for ListObjects (v1).
type ListBucketResult struct {
	XMLName        xml.Name       `xml:"ListBucketResult"`
	Xmlns          string         `xml:"xmlns,attr"`
	Name           string         `xml:"Name"`
	Prefix         string         `xml:"Prefix"`
	Marker         string         `xml:"Marker,omitempty"`
	MaxKeys        int            `xml:"MaxKeys"`
	Delimiter      string         `xml:"Delimiter,omitempty"`
	IsTruncated    bool           `xml:"IsTruncated"`
	Contents       []S3Object     `xml:"Contents,omitempty"`
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes,omitempty"`
	NextMarker     string         `xml:"NextMarker,omitempty"`
}

// ListBucketResultV2 is the response for ListObjectsV2.
type ListBucketResultV2 struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	Xmlns                 string         `xml:"xmlns,attr"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	StartAfter            string         `xml:"StartAfter,omitempty"`
	ContinuationToken     string         `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	MaxKeys               int            `xml:"MaxKeys"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	IsTruncated           bool           `xml:"IsTruncated"`
	Contents              []S3Object     `xml:"Contents,omitempty"`
	CommonPrefixes        []CommonPrefix `xml:"CommonPrefixes,omitempty"`
	KeyCount              int            `xml:"KeyCount"`
}

// S3Object represents an object in list responses.
type S3Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// CommonPrefix represents a common prefix in list responses.
type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// CopyObjectResult is the response for CopyObject.
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	Xmlns        string   `xml:"xmlns,attr"`
	LastModified string   `xml:"LastModified"`
	ETag         string   `xml:"ETag"`
}

// DeleteResult is the response for DeleteObject.
type DeleteResult struct {
	XMLName               xml.Name `xml:"DeleteResult"`
	Xmlns                 string   `xml:"xmlns,attr"`
	DeleteMarker          bool     `xml:"DeleteMarker,omitempty"`
	DeleteMarkerVersionID string   `xml:"DeleteMarkerVersionId,omitempty"`
	VersionID             string   `xml:"VersionId,omitempty"`
}

// ListVersionsResult is the response for ListObjectVersions.
type ListVersionsResult struct {
	XMLName             xml.Name          `xml:"ListVersionsResult"`
	Xmlns               string            `xml:"xmlns,attr"`
	Name                string            `xml:"Name"`
	Prefix              string            `xml:"Prefix"`
	KeyMarker           string            `xml:"KeyMarker,omitempty"`
	VersionIdMarker     string            `xml:"VersionIdMarker,omitempty"`
	NextKeyMarker       string            `xml:"NextKeyMarker,omitempty"`
	NextVersionIdMarker string            `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys             int               `xml:"MaxKeys"`
	Delimiter           string            `xml:"Delimiter,omitempty"`
	IsTruncated         bool              `xml:"IsTruncated"`
	Versions            []S3ObjectVersion `xml:"Version,omitempty"`
	DeleteMarkers       []S3DeleteMarker  `xml:"DeleteMarker,omitempty"`
	CommonPrefixes      []CommonPrefix    `xml:"CommonPrefixes,omitempty"`
}

// S3ObjectVersion represents an object version in list versions responses.
type S3ObjectVersion struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// S3DeleteMarker represents a delete marker in list versions responses.
type S3DeleteMarker struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
}

// =============================================================================
// Handler Methods
// =============================================================================

// PutObject handles PUT /{bucket}/{key} requests.
func (h *ObjectHandler) PutObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Get content length
	contentLength := r.ContentLength
	if contentLength < 0 {
		writeError(w, S3Error{
			Code:           "MissingContentLength",
			Message:        "You must provide the Content-Length HTTP header.",
			HTTPStatusCode: http.StatusLengthRequired,
		})
		return
	}

	// Get content type
	contentType := r.Header.Get("Content-Type")

	// Parse metadata from x-amz-meta-* headers
	metadata := parseMetadata(r)

	// Store object
	output, err := h.objectService.PutObject(ctx, service.PutObjectInput{
		BucketName:  bucketName,
		Key:         objectKey,
		Body:        r.Body,
		Size:        contentLength,
		ContentType: contentType,
		Metadata:    metadata,
		OwnerID:     userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, objectKey)
		return
	}

	// Success response
	w.Header().Set("ETag", output.ETag)
	if output.VersionID != "" && output.VersionID != "null" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}
	w.WriteHeader(http.StatusOK)
}

// GetObject handles GET /{bucket}/{key} requests.
func (h *ObjectHandler) GetObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Parse version ID
	versionID := r.URL.Query().Get("versionId")

	// Parse range header
	var byteRange *service.ByteRange
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var err error
		byteRange, err = parseRangeHeader(rangeHeader)
		if err != nil {
			writeError(w, S3Error{
				Code:           "InvalidRange",
				Message:        "The requested range is not satisfiable.",
				HTTPStatusCode: http.StatusRequestedRangeNotSatisfiable,
			})
			return
		}
	}

	// Get object
	output, err := h.objectService.GetObject(ctx, service.GetObjectInput{
		BucketName: bucketName,
		Key:        objectKey,
		VersionID:  versionID,
		OwnerID:    userCtx.UserID,
		Range:      byteRange,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, objectKey)
		return
	}
	defer output.Body.Close()

	// Set response headers
	w.Header().Set("Content-Type", output.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(output.ContentLength, 10))
	w.Header().Set("ETag", output.ETag)
	w.Header().Set("Last-Modified", output.LastModified.UTC().Format(http.TimeFormat))

	if output.VersionID != "" && output.VersionID != "null" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}

	// Set metadata headers
	for key, value := range output.Metadata {
		w.Header().Set("x-amz-meta-"+key, value)
	}

	// Handle range response
	if output.ContentRange != "" {
		w.Header().Set("Content-Range", output.ContentRange)
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// Stream content
	io.Copy(w, output.Body)
}

// HeadObject handles HEAD /{bucket}/{key} requests.
func (h *ObjectHandler) HeadObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Parse version ID
	versionID := r.URL.Query().Get("versionId")

	// Get object metadata
	output, err := h.objectService.HeadObject(ctx, service.HeadObjectInput{
		BucketName: bucketName,
		Key:        objectKey,
		VersionID:  versionID,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, objectKey)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", output.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(output.ContentLength, 10))
	w.Header().Set("ETag", output.ETag)
	w.Header().Set("Last-Modified", output.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("x-amz-storage-class", string(output.StorageClass))

	if output.VersionID != "" && output.VersionID != "null" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}

	// Set metadata headers
	for key, value := range output.Metadata {
		w.Header().Set("x-amz-meta-"+key, value)
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteObject handles DELETE /{bucket}/{key} requests.
func (h *ObjectHandler) DeleteObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Parse version ID
	versionID := r.URL.Query().Get("versionId")

	// Delete object
	output, err := h.objectService.DeleteObject(ctx, service.DeleteObjectInput{
		BucketName: bucketName,
		Key:        objectKey,
		VersionID:  versionID,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, objectKey)
		return
	}

	// Set response headers
	if output.DeleteMarker {
		w.Header().Set("x-amz-delete-marker", "true")
	}
	if output.VersionID != "" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListObjects handles GET /{bucket} requests (v1).
func (h *ObjectHandler) ListObjects(w http.ResponseWriter, r *http.Request, bucketName string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	query := r.URL.Query()

	// Check if this is v2
	if query.Get("list-type") == "2" {
		h.ListObjectsV2(w, r, bucketName)
		return
	}

	// Parse parameters
	maxKeys, _ := strconv.Atoi(query.Get("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// List objects
	output, err := h.objectService.ListObjects(ctx, service.ListObjectsInput{
		BucketName: bucketName,
		Prefix:     query.Get("prefix"),
		Delimiter:  query.Get("delimiter"),
		Marker:     query.Get("marker"),
		MaxKeys:    maxKeys,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, "")
		return
	}

	// Build response
	contents := make([]S3Object, len(output.Contents))
	for i, obj := range output.Contents {
		contents[i] = S3Object{
			Key:          obj.Key,
			LastModified: formatS3Time(obj.LastModified),
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: string(obj.StorageClass),
		}
	}

	commonPrefixes := make([]CommonPrefix, len(output.CommonPrefixes))
	for i, prefix := range output.CommonPrefixes {
		commonPrefixes[i] = CommonPrefix{Prefix: prefix}
	}

	response := ListBucketResult{
		Xmlns:          "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:           bucketName,
		Prefix:         output.Prefix,
		Marker:         query.Get("marker"),
		MaxKeys:        output.MaxKeys,
		Delimiter:      output.Delimiter,
		IsTruncated:    output.IsTruncated,
		Contents:       contents,
		CommonPrefixes: commonPrefixes,
		NextMarker:     output.NextMarker,
	}

	writeXML(w, http.StatusOK, response)
}

// ListObjectsV2 handles GET /{bucket}?list-type=2 requests.
func (h *ObjectHandler) ListObjectsV2(w http.ResponseWriter, r *http.Request, bucketName string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	query := r.URL.Query()

	// Parse parameters
	maxKeys, _ := strconv.Atoi(query.Get("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// List objects
	output, err := h.objectService.ListObjects(ctx, service.ListObjectsInput{
		BucketName:        bucketName,
		Prefix:            query.Get("prefix"),
		Delimiter:         query.Get("delimiter"),
		StartAfter:        query.Get("start-after"),
		ContinuationToken: query.Get("continuation-token"),
		MaxKeys:           maxKeys,
		OwnerID:           userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, "")
		return
	}

	// Build response
	contents := make([]S3Object, len(output.Contents))
	for i, obj := range output.Contents {
		contents[i] = S3Object{
			Key:          obj.Key,
			LastModified: formatS3Time(obj.LastModified),
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: string(obj.StorageClass),
		}
	}

	commonPrefixes := make([]CommonPrefix, len(output.CommonPrefixes))
	for i, prefix := range output.CommonPrefixes {
		commonPrefixes[i] = CommonPrefix{Prefix: prefix}
	}

	response := ListBucketResultV2{
		Xmlns:                 "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:                  bucketName,
		Prefix:                output.Prefix,
		StartAfter:            query.Get("start-after"),
		ContinuationToken:     query.Get("continuation-token"),
		NextContinuationToken: output.NextContinuationToken,
		MaxKeys:               output.MaxKeys,
		Delimiter:             output.Delimiter,
		IsTruncated:           output.IsTruncated,
		Contents:              contents,
		CommonPrefixes:        commonPrefixes,
		KeyCount:              output.KeyCount,
	}

	writeXML(w, http.StatusOK, response)
}

// ListObjectVersions handles GET /{bucket}?versions requests.
func (h *ObjectHandler) ListObjectVersions(w http.ResponseWriter, r *http.Request, bucketName string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	query := r.URL.Query()

	// Parse parameters
	maxKeys, _ := strconv.Atoi(query.Get("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// List versions
	output, err := h.objectService.ListObjectVersions(ctx, service.ListObjectVersionsInput{
		BucketName:      bucketName,
		Prefix:          query.Get("prefix"),
		Delimiter:       query.Get("delimiter"),
		KeyMarker:       query.Get("key-marker"),
		VersionIDMarker: query.Get("version-id-marker"),
		MaxKeys:         maxKeys,
		OwnerID:         userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, bucketName, "")
		return
	}

	// Build response
	versions := make([]S3ObjectVersion, len(output.Versions))
	for i, ver := range output.Versions {
		versions[i] = S3ObjectVersion{
			Key:          ver.Key,
			VersionId:    ver.VersionID,
			IsLatest:     ver.IsLatest,
			LastModified: formatS3Time(ver.LastModified),
			ETag:         ver.ETag,
			Size:         ver.Size,
			StorageClass: string(ver.StorageClass),
		}
	}

	deleteMarkers := make([]S3DeleteMarker, len(output.DeleteMarkers))
	for i, dm := range output.DeleteMarkers {
		deleteMarkers[i] = S3DeleteMarker{
			Key:          dm.Key,
			VersionId:    dm.VersionID,
			IsLatest:     dm.IsLatest,
			LastModified: formatS3Time(dm.LastModified),
		}
	}

	commonPrefixes := make([]CommonPrefix, len(output.CommonPrefixes))
	for i, prefix := range output.CommonPrefixes {
		commonPrefixes[i] = CommonPrefix{Prefix: prefix}
	}

	response := ListVersionsResult{
		Xmlns:               "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:                bucketName,
		Prefix:              output.Prefix,
		KeyMarker:           output.KeyMarker,
		VersionIdMarker:     output.VersionIDMarker,
		NextKeyMarker:       output.NextKeyMarker,
		NextVersionIdMarker: output.NextVersionIDMarker,
		MaxKeys:             output.MaxKeys,
		Delimiter:           output.Delimiter,
		IsTruncated:         output.IsTruncated,
		Versions:            versions,
		DeleteMarkers:       deleteMarkers,
		CommonPrefixes:      commonPrefixes,
	}

	writeXML(w, http.StatusOK, response)
}

// CopyObject handles PUT /{bucket}/{key} requests with x-amz-copy-source header.
func (h *ObjectHandler) CopyObject(w http.ResponseWriter, r *http.Request, destBucket, destKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	copySource := r.Header.Get("x-amz-copy-source")
	if copySource == "" {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Missing x-amz-copy-source header.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Parse copy source: /bucket/key or bucket/key
	copySource, _ = url.PathUnescape(copySource)
	copySource = strings.TrimPrefix(copySource, "/")
	parts := strings.SplitN(copySource, "/", 2)
	if len(parts) != 2 {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Invalid x-amz-copy-source header.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	sourceBucket := parts[0]
	sourceKey := parts[1]

	// Check for version ID in source
	var sourceVersionID string
	if idx := strings.Index(sourceKey, "?versionId="); idx != -1 {
		sourceVersionID = sourceKey[idx+11:]
		sourceKey = sourceKey[:idx]
	}

	// Get metadata directive
	metadataDirective := r.Header.Get("x-amz-metadata-directive")
	if metadataDirective == "" {
		metadataDirective = "COPY"
	}

	// Get content type override
	contentType := r.Header.Get("Content-Type")

	// Parse new metadata
	var metadata map[string]string
	if metadataDirective == "REPLACE" {
		metadata = parseMetadata(r)
	}

	// Copy object
	output, err := h.objectService.CopyObject(ctx, service.CopyObjectInput{
		SourceBucket:      sourceBucket,
		SourceKey:         sourceKey,
		SourceVersionID:   sourceVersionID,
		DestBucket:        destBucket,
		DestKey:           destKey,
		ContentType:       contentType,
		Metadata:          metadata,
		MetadataDirective: metadataDirective,
		OwnerID:           userCtx.UserID,
	})

	if err != nil {
		h.handleObjectError(w, err, destBucket, destKey)
		return
	}

	// Set version ID header
	if output.VersionID != "" && output.VersionID != "null" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}

	// Return XML response
	response := CopyObjectResult{
		Xmlns:        "http://s3.amazonaws.com/doc/2006-03-01/",
		LastModified: formatS3Time(output.LastModified),
		ETag:         output.ETag,
	}

	writeXML(w, http.StatusOK, response)
}

// =============================================================================
// Helper Methods
// =============================================================================

// parseMetadata extracts x-amz-meta-* headers into a map.
func parseMetadata(r *http.Request) map[string]string {
	metadata := make(map[string]string)
	for key, values := range r.Header {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "x-amz-meta-") && len(values) > 0 {
			metaKey := strings.TrimPrefix(lowerKey, "x-amz-meta-")
			metadata[metaKey] = values[0]
		}
	}
	return metadata
}

// parseRangeHeader parses a Range header into start/end bytes.
func parseRangeHeader(rangeHeader string) (*service.ByteRange, error) {
	// Format: bytes=start-end
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, fmt.Errorf("invalid range format")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format")
	}

	var start, end int64
	var err error

	if parts[0] != "" {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, err
		}
	}

	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, err
		}
	} else {
		// If end is not specified, we need to handle it in the service
		end = -1
	}

	return &service.ByteRange{Start: start, End: end}, nil
}

// handleObjectError maps service errors to S3 error responses.
func (h *ObjectHandler) handleObjectError(w http.ResponseWriter, err error, bucket, key string) {
	var s3Err S3Error
	resource := "/" + bucket
	if key != "" {
		resource += "/" + key
	}

	switch {
	case errors.Is(err, domain.ErrBucketNotFound):
		s3Err = ErrNoSuchBucket
	case errors.Is(err, domain.ErrObjectNotFound):
		s3Err = S3Error{
			Code:           "NoSuchKey",
			Message:        "The specified key does not exist.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrObjectDeleted):
		s3Err = S3Error{
			Code:           "NoSuchKey",
			Message:        "The specified key does not exist.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrObjectKeyEmpty):
		s3Err = S3Error{
			Code:           "InvalidArgument",
			Message:        "Object key cannot be empty.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrObjectKeyTooLong):
		s3Err = S3Error{
			Code:           "KeyTooLongError",
			Message:        "Your key is too long.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrInvalidVersionID):
		s3Err = S3Error{
			Code:           "InvalidArgument",
			Message:        "Invalid version id specified.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, service.ErrBucketAccessDenied):
		s3Err = ErrAccessDenied
	default:
		h.logger.Error().Err(err).Str("bucket", bucket).Str("key", key).Msg("unhandled error")
		s3Err = ErrInternalError
	}

	s3Err.Resource = resource
	writeError(w, s3Err)
}
