package nexra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Shared vectors: the backend test asserts the server signs these the same way.
const (
	secret = "whsec_sandbox_9f2c4e7a1b3d5f60718293a4b5c6d7e8"
	body   = `{"id":"evt_3f9a1c2b7d4e5f60718293a4","type":"sandbox.test","version":"2026-10-01","createdAt":"2026-10-01T09:31:12.000Z","mode":"test","workspaceId":"cmworkspace01","data":{"message":"Test event from Nexra"}}`
	stamp  = int64(1790847072)
	header = "t=1790847072,v1=831b5a6c13d544c4696629d2122da6c37b661ec33a4ef6d8ada910dcf283b089"
)

var at = time.Unix(stamp, 0)

// A complete Execution, as the API returns it.
const executionJSON = `{"id":"exec_1","integrationId":"int_1","flowId":"flow_1","flowVersionId":"fv_1","triggerType":"api","triggerReference":null,"status":"queued","correlationId":"corr-1","idempotencyKey":"run-1","startedAt":null,"completedAt":null,"durationMs":null,"recordsReceived":0,"recordsProcessed":0,"recordsSucceeded":0,"recordsFailed":0,"recordsSkipped":0,"retryCount":0,"deadlineAt":null,"createdAt":"2026-10-01T09:31:12.000Z","updatedAt":"2026-10-01T09:31:12.000Z"}`

func TestVerifiesTheServersVector(t *testing.T) {
	if !VerifyWebhookSignature([]byte(body), header, secret, VerifyOptions{Now: at}) {
		t.Fatal("valid signature refused")
	}
	if got := SignWebhookPayload([]byte(body), secret, at); got != header {
		t.Fatalf("signature %q, want %q", got, header)
	}
}

func TestRefusesBadSignatures(t *testing.T) {
	cases := []struct {
		name, body, header, secret string
		options                    VerifyOptions
	}{
		{"tampered", body + " ", header, secret, VerifyOptions{Now: at}},
		{"wrong secret", body, header, "whsec_other", VerifyOptions{Now: at}},
		{"stale", body, header, secret, VerifyOptions{Now: at.Add(301 * time.Second)}},
		{"no timestamp", body, "v1=abc", secret, VerifyOptions{Now: at}},
		{"bad hex", body, "t=1790847072,v1=zz", secret, VerifyOptions{Now: at}},
		{"missing", body, "", secret, VerifyOptions{Now: at}},
	}
	for _, c := range cases {
		if VerifyWebhookSignature([]byte(c.body), c.header, c.secret, c.options) {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if !VerifyWebhookSignature([]byte(body), header, secret, VerifyOptions{Now: at.Add(600 * time.Second), Tolerance: 900 * time.Second}) {
		t.Error("wider tolerance refused")
	}
}

func TestConstructEvent(t *testing.T) {
	event, err := ConstructEvent([]byte(body), header, secret, VerifyOptions{Now: at})
	if err != nil || event.Type != "sandbox.test" || event.WorkspaceID != "cmworkspace01" {
		t.Fatalf("event %+v, err %v", event, err)
	}
	if _, err := ConstructEvent([]byte(body), "t=1,v1=00", secret, VerifyOptions{}); !errors.Is(err, ErrWebhookVerification) {
		t.Fatalf("err %v", err)
	}
}

func TestSignsTriggerRequests(t *testing.T) {
	headers, err := SignTriggerRequest([]byte(`{"order":1}`), "nxwhsec_test", "evt-00000001", at)
	if err != nil {
		t.Fatal(err)
	}
	if headers["X-Nexra-Signature"] != "v1=94c5df1502045b1c038e5ac229046bd9ebcbfde94410b519e0d0f2188afdda58" {
		t.Fatalf("signature %s", headers["X-Nexra-Signature"])
	}
	if headers["X-Nexra-Timestamp"] != "1790847072" {
		t.Fatal("timestamp")
	}
	if _, err := SignTriggerRequest([]byte("{}"), "s", "short", at); err == nil {
		t.Fatal("short event id accepted")
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc, options Options) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options.BaseURL = server.URL + "/"
	if options.Token == "" {
		options.Token = "tok_123"
	}
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func TestSendsTokenWorkspaceAndClientHeaders(t *testing.T) {
	var seen *http.Request
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		writeJSON(w, 200, []any{})
	}, Options{WorkspaceID: "ws_1"})
	_, _, err := client.IntegrationsAPI.ListIntegrations(context.Background()).Execute()
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/api/console/connect/integrations" {
		t.Errorf("path %s", seen.URL.Path)
	}
	if seen.Header.Get("Authorization") != "Bearer tok_123" || seen.Header.Get("X-Workspace-Id") != "ws_1" {
		t.Errorf("headers %v", seen.Header)
	}
	if !strings.HasPrefix(seen.Header.Get("X-Nexra-Client"), "nexra-sdk-go/") {
		t.Errorf("client header %q", seen.Header.Get("X-Nexra-Client"))
	}
}

func TestNeedsAToken(t *testing.T) {
	t.Setenv("NEXRA_TOKEN", "")
	if _, err := NewClient(Options{}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err %v", err)
	}
}

type recordingTransport struct {
	statuses []int
	headers  []map[string]string
	calls    int
	bodies   []string
	fail     error
}

func (r *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := r.calls
	r.calls++
	if request.Body != nil {
		data, _ := io.ReadAll(request.Body)
		r.bodies = append(r.bodies, string(data))
	}
	if r.fail != nil {
		return nil, r.fail
	}
	if index >= len(r.statuses) {
		index = len(r.statuses) - 1
	}
	header := http.Header{}
	if index < len(r.headers) {
		for k, v := range r.headers[index] {
			header.Set(k, v)
		}
	}
	return &http.Response{StatusCode: r.statuses[index], Header: header, Body: io.NopCloser(strings.NewReader("{}")), Request: request}, nil
}

func retrying(base http.RoundTripper, max int, waits *[]time.Duration) *RetryTransport {
	return &RetryTransport{
		Base:    base,
		Options: RetryOptions{MaxRetries: max, BaseDelay: 100 * time.Millisecond, MaxDelay: 20 * time.Second},
		Sleep: func(_ *http.Request, wait time.Duration) error {
			if waits != nil {
				*waits = append(*waits, wait)
			}
			return nil
		},
	}
}

func TestRetriesGetsAndHonoursRetryAfter(t *testing.T) {
	base := &recordingTransport{statuses: []int{503, 500, 200}, headers: []map[string]string{{"Retry-After": "2"}}}
	var waits []time.Duration
	request, _ := http.NewRequest("GET", "https://x.test/a", nil)
	response, err := retrying(base, 3, &waits).RoundTrip(request)
	if err != nil || response.StatusCode != 200 || base.calls != 3 {
		t.Fatalf("status %v calls %d err %v", response, base.calls, err)
	}
	if waits[0] != 2*time.Second || waits[1] < 100*time.Millisecond || waits[1] > 200*time.Millisecond {
		t.Fatalf("waits %v", waits)
	}
}

func TestRetriesPostsOnlyWithIdempotencyKeyOr429(t *testing.T) {
	run := func(payload string, statuses []int) (int, int) {
		base := &recordingTransport{statuses: statuses}
		request, _ := http.NewRequest("POST", "https://x.test/a", strings.NewReader(payload))
		response, _ := retrying(base, 2, nil).RoundTrip(request)
		for _, sent := range base.bodies {
			if sent != payload {
				panic("body not replayed")
			}
		}
		return base.calls, response.StatusCode
	}
	check := func(name string, calls, status, wantCalls, wantStatus int) {
		if calls != wantCalls || status != wantStatus {
			t.Errorf("%s: calls %d status %d, want %d %d", name, calls, status, wantCalls, wantStatus)
		}
	}
	c, s := run(`{"a":1}`, []int{500, 200})
	check("plain post on 500", c, s, 1, 500)
	c, s = run(`{"idempotencyKey":"k-12345678"}`, []int{500, 200})
	check("keyed post on 500", c, s, 2, 200)
	c, s = run(`{"a":1}`, []int{429, 200})
	check("plain post on 429", c, s, 2, 200)
	c, s = run(`{"idempotencyKey":"k-12345678"}`, []int{500, 500, 500, 500})
	check("gives up", c, s, 3, 500)
}

func TestRetriesNetworkErrorsThenGivesUp(t *testing.T) {
	base := &recordingTransport{fail: errors.New("network down")}
	request, _ := http.NewRequest("GET", "https://x.test/a", nil)
	if _, err := retrying(base, 2, nil).RoundTrip(request); err == nil || base.calls != 3 {
		t.Fatalf("calls %d err %v", base.calls, err)
	}
}

func TestBackoffAndRetryAfter(t *testing.T) {
	options := RetryOptions{BaseDelay: 500 * time.Millisecond, MaxDelay: 2 * time.Second}
	one := func() float64 { return 1 }
	zero := func() float64 { return 0 }
	if BackoffDelay(1, options, one) != 500*time.Millisecond || BackoffDelay(3, options, one) != 2*time.Second || BackoffDelay(9, options, zero) != time.Second {
		t.Fatal("backoff")
	}
	if wait, ok := RetryAfter("3", time.Now()); !ok || wait != 3*time.Second {
		t.Fatal("seconds")
	}
	if wait, ok := RetryAfter(time.Unix(10, 0).UTC().Format(http.TimeFormat), time.Unix(4, 0)); !ok || wait != 6*time.Second {
		t.Fatalf("date %v", wait)
	}
	if _, ok := RetryAfter("soon", time.Now()); ok {
		t.Fatal("garbage")
	}
}

func TestStartRunAddsKeysAndReturnsTypedErrors(t *testing.T) {
	var bodies []map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var decoded map[string]any
		json.NewDecoder(r.Body).Decode(&decoded)
		bodies = append(bodies, decoded)
		if len(bodies) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			io.WriteString(w, executionJSON)
			return
		}
		writeJSON(w, 409, map[string]any{"error": "request_failed", "message": "Flow is paused.", "statusCode": 409, "requestId": "req_1", "path": "/x", "code": "flow_paused"})
	}, Options{DisableRetries: true})
	execution, err := client.StartRun(context.Background(), "flow_1", "sandbox", StartRunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if execution.GetId() != "exec_1" {
		t.Fatalf("id %q", execution.GetId())
	}
	key := regexp.MustCompile(`^run-[0-9a-f]{32}$`)
	if !key.MatchString(bodies[0]["idempotencyKey"].(string)) || !strings.HasPrefix(bodies[0]["correlationId"].(string), "corr-") {
		t.Fatalf("body %v", bodies[0])
	}
	_, err = client.StartRun(context.Background(), "flow_1", "sandbox", StartRunOptions{IdempotencyKey: "mine-0001"})
	var apiError *APIError
	if !errors.As(err, &apiError) || !errors.Is(err, ErrConflict) {
		t.Fatalf("err %v", err)
	}
	if apiError.Status != 409 || apiError.Code != "flow_paused" || apiError.RequestID != "req_1" || apiError.Message != "Flow is paused." {
		t.Fatalf("api error %+v", apiError)
	}
	if bodies[1]["idempotencyKey"] != "mine-0001" {
		t.Fatal("own key not sent")
	}
}

func TestParseErrorKindsAndNonNexraBodies(t *testing.T) {
	kinds := map[int]error{400: ErrBadRequest, 422: ErrBadRequest, 401: ErrAuthentication, 403: ErrPermission, 404: ErrNotFound, 409: ErrConflict, 429: ErrRateLimited, 500: ErrServer, 503: ErrServer}
	for status, kind := range kinds {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", "req_hdr")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(status)
			io.WriteString(w, "<html>Edge says no</html>")
		}, Options{Retry: &RetryOptions{MaxRetries: 1}})
		_, response, err := client.IntegrationsAPI.ListIntegrations(context.Background()).Execute()
		err = ParseError(response, err)
		var apiError *APIError
		if !errors.Is(err, kind) || !errors.As(err, &apiError) {
			t.Fatalf("%d: err %v", status, err)
		}
		if apiError.RequestID != "req_hdr" || !strings.Contains(apiError.Message, "Edge says no") {
			t.Fatalf("%d: %+v", status, apiError)
		}
	}
	if ParseError(nil, nil) != nil {
		t.Fatal("nil error")
	}
	plain := errors.New("dial failed")
	if ParseError(nil, plain) != plain {
		t.Fatal("network error changed")
	}
}

func TestRateLimitAfterRetriesIsTyped(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		writeJSON(w, 429, map[string]any{"error": "request_failed", "message": "Slow down.", "statusCode": 429, "requestId": "req_9", "path": "/x"})
	}, Options{Retry: &RetryOptions{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}})
	_, response, err := client.IntegrationsAPI.ListIntegrations(context.Background()).Execute()
	err = ParseError(response, err)
	if !errors.Is(err, ErrRateLimited) || calls.Load() != 3 {
		t.Fatalf("calls %d err %v", calls.Load(), err)
	}
}

func TestPaginateFollowsCursor(t *testing.T) {
	pages := map[string]Page[int]{"": {Items: []int{1, 2}, NextCursor: "c2"}, "c2": {Items: []int{3}, NextCursor: "c3"}, "c3": {}}
	var cursors []string
	items, err := Collect(Paginate(context.Background(), func(_ context.Context, cursor string) (Page[int], error) {
		cursors = append(cursors, cursor)
		return pages[cursor], nil
	}), 0)
	if err != nil || len(items) != 3 || items[2] != 3 || strings.Join(cursors, ",") != ",c2,c3" {
		t.Fatalf("items %v cursors %v err %v", items, cursors, err)
	}
	failing := errors.New("boom")
	_, err = Collect(Paginate(context.Background(), func(context.Context, string) (Page[int], error) { return Page[int]{}, failing }), 0)
	if !errors.Is(err, failing) {
		t.Fatal("error not surfaced")
	}
}

func TestIdempotencyKeyFormat(t *testing.T) {
	a := NewIdempotencyKey("")
	if a == NewIdempotencyKey("") || !regexp.MustCompile(`^[A-Za-z0-9._:-]{8,160}$`).MatchString(a) {
		t.Fatal(a)
	}
}
