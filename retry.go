package nexra

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// RetryOptions set how failed requests are retried.
//
// A request is retried when the network fails or the answer is 408, 425,
// 429, 500, 502, 503 or 504. To keep retries safe, a request that changes
// data (POST or PATCH) is only retried when Nexra did not act on it (429) or
// when its JSON body carries an idempotencyKey, which makes a repeat return
// the first result. Retry-After is honoured.
type RetryOptions struct {
	// MaxRetries after the first attempt. 0 turns retries off.
	MaxRetries int
	// BaseDelay is the first backoff.
	BaseDelay time.Duration
	// MaxDelay caps a single wait.
	MaxDelay time.Duration
}

// DefaultRetryOptions are 3 retries from 500ms, capped at 20s.
var DefaultRetryOptions = RetryOptions{MaxRetries: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 20 * time.Second}

var retryableStatus = map[int]bool{408: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true}
var safeMethods = map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true, "PUT": true, "DELETE": true}

// BackoffDelay is the wait before retry attempt (1-based): the upper half of
// an exponential ceiling, with jitter. random returns a number in [0, 1).
func BackoffDelay(attempt int, options RetryOptions, random func() float64) time.Duration {
	if random == nil {
		random = rand.Float64
	}
	ceiling := math.Min(float64(options.MaxDelay), float64(options.BaseDelay)*math.Pow(2, float64(attempt-1)))
	return time.Duration(ceiling/2 + random()*(ceiling/2))
}

// RetryAfter parses a Retry-After value (seconds or an HTTP date).
// The second result is false when the value is missing or not valid.
func RetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(math.Max(0, seconds) * float64(time.Second)), true
	}
	if date, err := http.ParseTime(value); err == nil {
		wait := date.Sub(now)
		if wait < 0 {
			wait = 0
		}
		return wait, true
	}
	return 0, false
}

// RetryTransport is an http.RoundTripper that applies Nexra's retry rules.
type RetryTransport struct {
	Base    http.RoundTripper
	Options RetryOptions
	// Sleep waits between attempts. Defaults to a timer that stops when the
	// request's context ends.
	Sleep func(request *http.Request, wait time.Duration) error
}

func hasIdempotencyKey(body []byte) bool {
	var decoded map[string]json.RawMessage
	if json.Unmarshal(body, &decoded) != nil {
		return false
	}
	_, ok := decoded["idempotencyKey"]
	return ok
}

func sleepWithContext(request *http.Request, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-request.Context().Done():
		return request.Context().Err()
	case <-timer.C:
		return nil
	}
}

// RoundTrip sends the request, retrying where it is safe.
func (t *RetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	sleep := t.Sleep
	if sleep == nil {
		sleep = sleepWithContext
	}
	var body []byte
	if request.Body != nil && request.Body != http.NoBody {
		read, err := io.ReadAll(request.Body)
		request.Body.Close()
		if err != nil {
			return nil, err
		}
		body = read
	}
	replayable := safeMethods[request.Method] || hasIdempotencyKey(body)

	for attempt := 0; ; attempt++ {
		current := request.Clone(request.Context())
		if body != nil {
			current.Body = io.NopCloser(bytes.NewReader(body))
			current.ContentLength = int64(len(body))
			current.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		}
		response, err := base.RoundTrip(current)
		retryable := replayable
		if err == nil {
			retryable = retryableStatus[response.StatusCode] && (replayable || response.StatusCode == http.StatusTooManyRequests)
		}
		if !retryable || attempt >= t.Options.MaxRetries || request.Context().Err() != nil {
			return response, err
		}
		wait := BackoffDelay(attempt+1, t.Options, nil)
		if response != nil {
			if after, ok := RetryAfter(response.Header.Get("Retry-After"), time.Now()); ok {
				wait = after
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		if t.Options.MaxDelay > 0 && wait > t.Options.MaxDelay {
			wait = t.Options.MaxDelay
		}
		if err := sleep(request, wait); err != nil {
			return nil, err
		}
	}
}
