package nexra

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/TochukwuAchebe/nexra-go/nexraapi"
)

// Kinds of refusal. Test for them with errors.Is:
//
//	if errors.Is(err, nexra.ErrNotFound) { ... }
var (
	ErrBadRequest     = errors.New("nexra: bad request")       // 400, 422
	ErrAuthentication = errors.New("nexra: not signed in")     // 401
	ErrPermission     = errors.New("nexra: not allowed")       // 403
	ErrNotFound       = errors.New("nexra: not found")         // 404
	ErrConflict       = errors.New("nexra: conflict")          // 409
	ErrRateLimited    = errors.New("nexra: too many requests") // 429
	ErrServer         = errors.New("nexra: server error")      // 5xx
)

// APIError is a refusal from Nexra, with its status, message, stable code
// (where one exists) and request id. Quote RequestID when you contact support.
type APIError struct {
	Status    int
	Message   string
	Code      string
	RequestID string
	// RetryAfter is the Retry-After header, for 429 answers.
	RetryAfter string
	Body       []byte
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("nexra: %d: %s", e.Status, e.Message)
	if e.RequestID != "" {
		text += " (request " + e.RequestID + ")"
	}
	return text
}

// Kind is the sentinel error for this status, or nil.
func (e *APIError) Kind() error {
	switch {
	case e.Status == 400 || e.Status == 422:
		return ErrBadRequest
	case e.Status == 401:
		return ErrAuthentication
	case e.Status == 403:
		return ErrPermission
	case e.Status == 404:
		return ErrNotFound
	case e.Status == 409:
		return ErrConflict
	case e.Status == 429:
		return ErrRateLimited
	case e.Status >= 500:
		return ErrServer
	}
	return nil
}

// Is lets errors.Is match the sentinel errors.
func (e *APIError) Is(target error) bool {
	kind := e.Kind()
	return kind != nil && kind == target
}

// NewAPIError builds an APIError from a status, headers and body in Nexra's
// error shape (other bodies are kept as text).
func NewAPIError(status int, header http.Header, body []byte) *APIError {
	apiError := &APIError{Status: status, Body: body}
	var decoded struct {
		Message   *string `json:"message"`
		Code      *string `json:"code"`
		RequestID *string `json:"requestId"`
	}
	if json.Unmarshal(body, &decoded) == nil {
		if decoded.Message != nil {
			apiError.Message = *decoded.Message
		}
		if decoded.Code != nil {
			apiError.Code = *decoded.Code
		}
		if decoded.RequestID != nil {
			apiError.RequestID = *decoded.RequestID
		}
	}
	if apiError.Message == "" {
		apiError.Message = fmt.Sprintf("Nexra answered %d.", status)
		if text := strings.TrimSpace(string(body)); text != "" {
			if len(text) > 200 {
				text = text[:200]
			}
			apiError.Message += " " + text
		}
	}
	if header != nil {
		if apiError.RequestID == "" {
			apiError.RequestID = header.Get("X-Request-ID")
		}
		apiError.RetryAfter = header.Get("Retry-After")
	}
	return apiError
}

// ParseError turns the error from a generated Execute() call into an
// *APIError when Nexra answered with a refusal. Other errors (network,
// context, decoding) are returned unchanged, and nil stays nil.
//
//	flow, response, err := client.FlowsAPI.GetFlow(ctx, id).Execute()
//	if err := nexra.ParseError(response, err); errors.Is(err, nexra.ErrNotFound) { ... }
func ParseError(response *http.Response, err error) error {
	if err == nil {
		return nil
	}
	var body []byte
	var generic *nexraapi.GenericOpenAPIError
	var genericValue nexraapi.GenericOpenAPIError
	switch {
	case errors.As(err, &generic):
		body = generic.Body()
	case errors.As(err, &genericValue):
		body = genericValue.Body()
	default:
		return err
	}
	if response == nil || response.StatusCode < 300 {
		return err
	}
	return NewAPIError(response.StatusCode, response.Header, body)
}
