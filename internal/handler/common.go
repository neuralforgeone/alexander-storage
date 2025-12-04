// Package handler provides HTTP handlers for Alexander Storage API.
package handler

import (
	"encoding/xml"
	"net/http"
	"time"
)

// Common S3 XML response types

// Owner represents the owner of a bucket or object.
type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

// writeXML writes an XML response with the given status code.
func writeXML(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Encode(v)
}

// writeError writes an S3-compatible error response.
func writeError(w http.ResponseWriter, err S3Error) {
	writeXML(w, err.HTTPStatusCode, ErrorResponse{
		Code:      err.Code,
		Message:   err.Message,
		Resource:  err.Resource,
		RequestID: err.RequestID,
	})
}

// ErrorResponse is the S3-compatible error response format.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// S3Error represents an S3-compatible error.
type S3Error struct {
	Code           string
	Message        string
	HTTPStatusCode int
	Resource       string
	RequestID      string
}

// Common S3 errors
var (
	ErrAccessDenied = S3Error{
		Code:           "AccessDenied",
		Message:        "Access Denied",
		HTTPStatusCode: http.StatusForbidden,
	}

	ErrBucketAlreadyExists = S3Error{
		Code:           "BucketAlreadyExists",
		Message:        "The requested bucket name is not available. The bucket namespace is shared by all users of the system.",
		HTTPStatusCode: http.StatusConflict,
	}

	ErrBucketAlreadyOwnedByYou = S3Error{
		Code:           "BucketAlreadyOwnedByYou",
		Message:        "Your previous request to create the named bucket succeeded and you already own it.",
		HTTPStatusCode: http.StatusConflict,
	}

	ErrBucketNotEmpty = S3Error{
		Code:           "BucketNotEmpty",
		Message:        "The bucket you tried to delete is not empty.",
		HTTPStatusCode: http.StatusConflict,
	}

	ErrNoSuchBucket = S3Error{
		Code:           "NoSuchBucket",
		Message:        "The specified bucket does not exist.",
		HTTPStatusCode: http.StatusNotFound,
	}

	ErrInvalidBucketName = S3Error{
		Code:           "InvalidBucketName",
		Message:        "The specified bucket is not valid.",
		HTTPStatusCode: http.StatusBadRequest,
	}

	ErrInternalError = S3Error{
		Code:           "InternalError",
		Message:        "We encountered an internal error. Please try again.",
		HTTPStatusCode: http.StatusInternalServerError,
	}

	ErrMalformedXML = S3Error{
		Code:           "MalformedXML",
		Message:        "The XML you provided was not well-formed or did not validate against our published schema.",
		HTTPStatusCode: http.StatusBadRequest,
	}

	ErrIllegalVersioningConfigurationException = S3Error{
		Code:           "IllegalVersioningConfigurationException",
		Message:        "The versioning configuration specified in the request is invalid.",
		HTTPStatusCode: http.StatusBadRequest,
	}
)

// formatS3Time formats a time in S3's expected format.
func formatS3Time(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
