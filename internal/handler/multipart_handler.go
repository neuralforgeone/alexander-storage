// Package handler provides HTTP handlers for Alexander Storage API.
package handler

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/auth"
	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/service"
)

// MultipartHandler handles multipart upload HTTP requests.
type MultipartHandler struct {
	multipartService *service.MultipartService
	logger           zerolog.Logger
}

// NewMultipartHandler creates a new MultipartHandler.
func NewMultipartHandler(multipartService *service.MultipartService, logger zerolog.Logger) *MultipartHandler {
	return &MultipartHandler{
		multipartService: multipartService,
		logger:           logger.With().Str("handler", "multipart").Logger(),
	}
}

// =============================================================================
// XML Types
// =============================================================================

// InitiateMultipartUploadResult is the response for InitiateMultipartUpload.
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Xmlns    string   `xml:"xmlns,attr"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

// CompleteMultipartUploadResult is the response for CompleteMultipartUpload.
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Xmlns    string   `xml:"xmlns,attr"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// CompleteMultipartUploadRequest is the request body for CompleteMultipartUpload.
type CompleteMultipartUploadRequest struct {
	XMLName xml.Name               `xml:"CompleteMultipartUpload"`
	Parts   []CompletedPartRequest `xml:"Part"`
}

// CompletedPartRequest represents a part in the completion request.
type CompletedPartRequest struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// ListMultipartUploadsResult is the response for ListMultipartUploads.
type ListMultipartUploadsResult struct {
	XMLName            xml.Name        `xml:"ListMultipartUploadsResult"`
	Xmlns              string          `xml:"xmlns,attr"`
	Bucket             string          `xml:"Bucket"`
	KeyMarker          string          `xml:"KeyMarker,omitempty"`
	UploadIdMarker     string          `xml:"UploadIdMarker,omitempty"`
	NextKeyMarker      string          `xml:"NextKeyMarker,omitempty"`
	NextUploadIdMarker string          `xml:"NextUploadIdMarker,omitempty"`
	Prefix             string          `xml:"Prefix,omitempty"`
	Delimiter          string          `xml:"Delimiter,omitempty"`
	MaxUploads         int             `xml:"MaxUploads"`
	IsTruncated        bool            `xml:"IsTruncated"`
	Uploads            []UploadElement `xml:"Upload,omitempty"`
	CommonPrefixes     []CommonPrefix  `xml:"CommonPrefixes,omitempty"`
}

// UploadElement represents an upload in list uploads response.
type UploadElement struct {
	Key          string `xml:"Key"`
	UploadId     string `xml:"UploadId"`
	Initiated    string `xml:"Initiated"`
	StorageClass string `xml:"StorageClass"`
}

// ListPartsResult is the response for ListParts.
type ListPartsResult struct {
	XMLName              xml.Name      `xml:"ListPartsResult"`
	Xmlns                string        `xml:"xmlns,attr"`
	Bucket               string        `xml:"Bucket"`
	Key                  string        `xml:"Key"`
	UploadId             string        `xml:"UploadId"`
	PartNumberMarker     int           `xml:"PartNumberMarker"`
	NextPartNumberMarker int           `xml:"NextPartNumberMarker,omitempty"`
	MaxParts             int           `xml:"MaxParts"`
	IsTruncated          bool          `xml:"IsTruncated"`
	Parts                []PartElement `xml:"Part,omitempty"`
	StorageClass         string        `xml:"StorageClass"`
}

// PartElement represents a part in list parts response.
type PartElement struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

// =============================================================================
// Handler Methods
// =============================================================================

// InitiateMultipartUpload handles POST /{bucket}/{key}?uploads requests.
func (h *MultipartHandler) InitiateMultipartUpload(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Get content type and metadata
	contentType := r.Header.Get("Content-Type")
	metadata := parseMetadata(r)
	if contentType != "" {
		metadata["Content-Type"] = contentType
	}

	// Get storage class
	storageClass := domain.StorageClass(r.Header.Get("x-amz-storage-class"))
	if storageClass == "" {
		storageClass = domain.StorageClassStandard
	}

	// Initiate upload
	output, err := h.multipartService.InitiateMultipartUpload(ctx, service.InitiateMultipartUploadInput{
		BucketName:   bucketName,
		Key:          objectKey,
		ContentType:  contentType,
		Metadata:     metadata,
		StorageClass: storageClass,
		OwnerID:      userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, objectKey)
		return
	}

	// Return XML response
	response := InitiateMultipartUploadResult{
		Xmlns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:   output.Bucket,
		Key:      output.Key,
		UploadId: output.UploadID,
	}

	writeXML(w, http.StatusOK, response)
}

// UploadPart handles PUT /{bucket}/{key}?partNumber=N&uploadId=X requests.
func (h *MultipartHandler) UploadPart(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	query := r.URL.Query()

	// Get upload ID
	uploadID := query.Get("uploadId")
	if uploadID == "" {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Missing uploadId parameter.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Get part number
	partNumberStr := query.Get("partNumber")
	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 || partNumber > 10000 {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Part number must be an integer between 1 and 10000.",
			HTTPStatusCode: http.StatusBadRequest,
		})
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

	// Upload part
	output, err := h.multipartService.UploadPart(ctx, service.UploadPartInput{
		BucketName: bucketName,
		Key:        objectKey,
		UploadID:   uploadID,
		PartNumber: partNumber,
		Body:       r.Body,
		Size:       contentLength,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, objectKey)
		return
	}

	// Set ETag header
	w.Header().Set("ETag", output.ETag)
	w.WriteHeader(http.StatusOK)
}

// CompleteMultipartUpload handles POST /{bucket}/{key}?uploadId=X requests.
func (h *MultipartHandler) CompleteMultipartUpload(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Get upload ID
	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Missing uploadId parameter.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Parse request body
	var req CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, S3Error{
			Code:           "MalformedXML",
			Message:        "The XML you provided was not well-formed.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Convert to domain parts
	parts := make([]domain.CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		parts[i] = domain.CompletedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		}
	}

	// Complete upload
	output, err := h.multipartService.CompleteMultipartUpload(ctx, service.CompleteMultipartUploadInput{
		BucketName: bucketName,
		Key:        objectKey,
		UploadID:   uploadID,
		Parts:      parts,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, objectKey)
		return
	}

	// Set version ID header if applicable
	if output.VersionID != "" && output.VersionID != "null" {
		w.Header().Set("x-amz-version-id", output.VersionID)
	}

	// Return XML response
	response := CompleteMultipartUploadResult{
		Xmlns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Location: output.Location,
		Bucket:   output.Bucket,
		Key:      output.Key,
		ETag:     output.ETag,
	}

	writeXML(w, http.StatusOK, response)
}

// AbortMultipartUpload handles DELETE /{bucket}/{key}?uploadId=X requests.
func (h *MultipartHandler) AbortMultipartUpload(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	// Get upload ID
	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Missing uploadId parameter.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Abort upload
	err := h.multipartService.AbortMultipartUpload(ctx, service.AbortMultipartUploadInput{
		BucketName: bucketName,
		Key:        objectKey,
		UploadID:   uploadID,
		OwnerID:    userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, objectKey)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListMultipartUploads handles GET /{bucket}?uploads requests.
func (h *MultipartHandler) ListMultipartUploads(w http.ResponseWriter, r *http.Request, bucketName string) {
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
	maxUploads, _ := strconv.Atoi(query.Get("max-uploads"))
	if maxUploads <= 0 {
		maxUploads = 1000
	}

	// List uploads
	output, err := h.multipartService.ListMultipartUploads(ctx, service.ListMultipartUploadsInput{
		BucketName:     bucketName,
		Prefix:         query.Get("prefix"),
		Delimiter:      query.Get("delimiter"),
		KeyMarker:      query.Get("key-marker"),
		UploadIDMarker: query.Get("upload-id-marker"),
		MaxUploads:     maxUploads,
		OwnerID:        userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, "")
		return
	}

	// Build response
	uploads := make([]UploadElement, len(output.Uploads))
	for i, u := range output.Uploads {
		uploads[i] = UploadElement{
			Key:          u.Key,
			UploadId:     u.UploadID,
			Initiated:    formatS3Time(u.Initiated),
			StorageClass: string(u.StorageClass),
		}
	}

	commonPrefixes := make([]CommonPrefix, len(output.CommonPrefixes))
	for i, prefix := range output.CommonPrefixes {
		commonPrefixes[i] = CommonPrefix{Prefix: prefix}
	}

	response := ListMultipartUploadsResult{
		Xmlns:              "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:             output.Bucket,
		KeyMarker:          output.KeyMarker,
		UploadIdMarker:     output.UploadIDMarker,
		NextKeyMarker:      output.NextKeyMarker,
		NextUploadIdMarker: output.NextUploadIDMarker,
		Prefix:             output.Prefix,
		Delimiter:          output.Delimiter,
		MaxUploads:         output.MaxUploads,
		IsTruncated:        output.IsTruncated,
		Uploads:            uploads,
		CommonPrefixes:     commonPrefixes,
	}

	writeXML(w, http.StatusOK, response)
}

// ListParts handles GET /{bucket}/{key}?uploadId=X requests.
func (h *MultipartHandler) ListParts(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	ctx := r.Context()

	// Get authenticated user from context
	userCtx, ok := auth.GetUserContext(ctx)
	if !ok {
		h.logger.Error().Msg("no user context found")
		writeError(w, ErrAccessDenied)
		return
	}

	query := r.URL.Query()

	// Get upload ID
	uploadID := query.Get("uploadId")
	if uploadID == "" {
		writeError(w, S3Error{
			Code:           "InvalidArgument",
			Message:        "Missing uploadId parameter.",
			HTTPStatusCode: http.StatusBadRequest,
		})
		return
	}

	// Parse parameters
	partNumberMarker, _ := strconv.Atoi(query.Get("part-number-marker"))
	maxParts, _ := strconv.Atoi(query.Get("max-parts"))
	if maxParts <= 0 {
		maxParts = 1000
	}

	// List parts
	output, err := h.multipartService.ListParts(ctx, service.ListPartsInput{
		BucketName:       bucketName,
		Key:              objectKey,
		UploadID:         uploadID,
		PartNumberMarker: partNumberMarker,
		MaxParts:         maxParts,
		OwnerID:          userCtx.UserID,
	})

	if err != nil {
		h.handleMultipartError(w, err, bucketName, objectKey)
		return
	}

	// Build response
	parts := make([]PartElement, len(output.Parts))
	for i, p := range output.Parts {
		parts[i] = PartElement{
			PartNumber:   p.PartNumber,
			LastModified: formatS3Time(p.LastModified),
			ETag:         p.ETag,
			Size:         p.Size,
		}
	}

	response := ListPartsResult{
		Xmlns:                "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:               output.Bucket,
		Key:                  output.Key,
		UploadId:             output.UploadID,
		PartNumberMarker:     output.PartNumberMarker,
		NextPartNumberMarker: output.NextPartNumberMarker,
		MaxParts:             output.MaxParts,
		IsTruncated:          output.IsTruncated,
		Parts:                parts,
		StorageClass:         string(output.StorageClass),
	}

	writeXML(w, http.StatusOK, response)
}

// =============================================================================
// Helper Methods
// =============================================================================

// handleMultipartError maps service errors to S3 error responses.
func (h *MultipartHandler) handleMultipartError(w http.ResponseWriter, err error, bucket, key string) {
	var s3Err S3Error
	resource := "/" + bucket
	if key != "" {
		resource += "/" + key
	}

	switch {
	case errors.Is(err, domain.ErrBucketNotFound):
		s3Err = ErrNoSuchBucket
	case errors.Is(err, domain.ErrMultipartUploadNotFound):
		s3Err = S3Error{
			Code:           "NoSuchUpload",
			Message:        "The specified multipart upload does not exist.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrMultipartUploadExpired):
		s3Err = S3Error{
			Code:           "NoSuchUpload",
			Message:        "The specified multipart upload has expired.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrMultipartUploadCompleted):
		s3Err = S3Error{
			Code:           "NoSuchUpload",
			Message:        "The specified multipart upload is already completed.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrMultipartUploadAborted):
		s3Err = S3Error{
			Code:           "NoSuchUpload",
			Message:        "The specified multipart upload has been aborted.",
			HTTPStatusCode: http.StatusNotFound,
		}
	case errors.Is(err, domain.ErrInvalidPartNumber):
		s3Err = S3Error{
			Code:           "InvalidArgument",
			Message:        "Part number must be between 1 and 10000.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrPartTooSmall):
		s3Err = S3Error{
			Code:           "EntityTooSmall",
			Message:        "Your proposed upload is smaller than the minimum allowed object size.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrPartTooLarge):
		s3Err = S3Error{
			Code:           "EntityTooLarge",
			Message:        "Your proposed upload exceeds the maximum allowed object size.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrPartNotFound):
		s3Err = S3Error{
			Code:           "InvalidPart",
			Message:        "One or more of the specified parts could not be found.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrPartETagMismatch):
		s3Err = S3Error{
			Code:           "InvalidPart",
			Message:        "One or more of the specified parts had invalid ETags.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrInvalidPartOrder):
		s3Err = S3Error{
			Code:           "InvalidPartOrder",
			Message:        "Parts must be specified in ascending order by part number.",
			HTTPStatusCode: http.StatusBadRequest,
		}
	case errors.Is(err, domain.ErrNoPartsProvided):
		s3Err = S3Error{
			Code:           "MalformedXML",
			Message:        "The XML you provided did not have the required number of parts.",
			HTTPStatusCode: http.StatusBadRequest,
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
	case errors.Is(err, service.ErrBucketAccessDenied):
		s3Err = ErrAccessDenied
	default:
		h.logger.Error().Err(err).Str("bucket", bucket).Str("key", key).Msg("unhandled error")
		s3Err = ErrInternalError
	}

	s3Err.Resource = resource
	writeError(w, s3Err)
}
