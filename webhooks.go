package nexra

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries Nexra's signature on event deliveries.
const SignatureHeader = "X-Amakora-Signature"

// DefaultTolerance is the oldest signature timestamp accepted.
const DefaultTolerance = 5 * time.Minute

// ErrWebhookVerification means the signature is missing, wrong or too old.
var ErrWebhookVerification = errors.New("nexra: the signature is missing, does not match, or is older than the tolerance")

// VerifyOptions change signature checks. The zero value uses DefaultTolerance and the current time.
type VerifyOptions struct {
	Tolerance time.Duration
	Now       time.Time
}

func hmacSHA256(secret string, parts ...[]byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	for _, part := range parts {
		mac.Write(part)
	}
	return mac.Sum(nil)
}

// VerifyWebhookSignature reports whether header is a valid, fresh signature
// of rawBody with secret. Nexra signs "<t>.<raw body>" with HMAC-SHA256 and
// sends "t=<unix seconds>,v1=<hex>". Verify the raw bytes before parsing them.
func VerifyWebhookSignature(rawBody []byte, header, secret string, options VerifyOptions) bool {
	if header == "" || secret == "" {
		return false
	}
	var timestamp int64
	haveTimestamp := false
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(part, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return false
			}
			timestamp, haveTimestamp = parsed, true
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if !haveTimestamp || len(signatures) == 0 {
		return false
	}
	tolerance := options.Tolerance
	if tolerance == 0 {
		tolerance = DefaultTolerance
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	if math.Abs(float64(now.Unix()-timestamp)) > tolerance.Seconds() {
		return false
	}
	expected := hmacSHA256(secret, []byte(strconv.FormatInt(timestamp, 10)+"."), rawBody)
	for _, signature := range signatures {
		received, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(received, expected) {
			return true
		}
	}
	return false
}

// Event is a Nexra event envelope. Data holds the event's own fields; decode
// it into the matching nexraapi.Event* model when you need them typed.
type Event struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Version     string          `json:"version"`
	CreatedAt   time.Time       `json:"createdAt"`
	Mode        string          `json:"mode"`
	WorkspaceID string          `json:"workspaceId"`
	Data        json.RawMessage `json:"data"`
}

// ConstructEvent verifies the signature and returns the parsed event, or an
// error that matches ErrWebhookVerification.
func ConstructEvent(rawBody []byte, header, secret string, options VerifyOptions) (*Event, error) {
	if !VerifyWebhookSignature(rawBody, header, secret, options) {
		return nil, ErrWebhookVerification
	}
	var event Event
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, fmt.Errorf("nexra: event body is not valid JSON: %w", err)
	}
	return &event, nil
}

// SignWebhookPayload signs a body the way Nexra does. Useful for testing your handler.
func SignWebhookPayload(rawBody []byte, secret string, timestamp time.Time) string {
	t := strconv.FormatInt(timestamp.Unix(), 10)
	return "t=" + t + ",v1=" + hex.EncodeToString(hmacSHA256(secret, []byte(t+"."), rawBody))
}

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// SignTriggerRequest returns the headers for a signed request to a Nexra
// webhook trigger (POST /api/connect/triggers/{publicId}). The signed bytes
// are "v1:<timestamp>:<eventID>:<raw body>". eventID must be unique per
// delivery: 8 to 128 of A-Z a-z 0-9 . _ : -
func SignTriggerRequest(rawBody []byte, secret, eventID string, timestamp time.Time) (map[string]string, error) {
	if !eventIDPattern.MatchString(eventID) {
		return nil, errors.New("nexra: eventID must be 8 to 128 characters of A-Z a-z 0-9 . _ : -")
	}
	t := strconv.FormatInt(timestamp.Unix(), 10)
	digest := hmacSHA256(secret, []byte("v1:"+t+":"+eventID+":"), rawBody)
	return map[string]string{
		"Content-Type":      "application/json",
		"X-Nexra-Event-Id":  eventID,
		"X-Nexra-Timestamp": t,
		"X-Nexra-Signature": "v1=" + hex.EncodeToString(digest),
	}, nil
}

// NewIdempotencyKey is the prefix and 32 random hex characters.
func NewIdempotencyKey(prefix string) string {
	if prefix == "" {
		prefix = "sdk"
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}
