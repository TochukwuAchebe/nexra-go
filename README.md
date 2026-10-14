# Nexra SDK for Go

A client for the Nexra platform API: integrations, flows, runs, connections,
triggers, reconciliation, approvals and signed event webhooks. Every
operation and model is generated from the published OpenAPI document into
the `nexraapi` package; the `nexra` package adds sign-in headers, retries with
backoff, typed errors, cursor pages, safe run starts and webhook signature
checks.

- Go 1.23 or later; no dependencies outside the standard library
- API version `2026-10-01`

## Install

```bash
go get github.com/TochukwuAchebe/nexra-go@v1.0.0
```

## Quickstart

Sign in once with the Nexra CLI (`nexra auth login`) to get an access token.
The token acts with your own role in the workspace.

```go
import (
	"context"
	"fmt"
	"log"
	"os"

	nexra "github.com/TochukwuAchebe/nexra-go"
)

ctx := context.Background()
client, err := nexra.NewClient(nexra.Options{
	Token:       os.Getenv("NEXRA_TOKEN"),        // the default
	WorkspaceID: os.Getenv("NEXRA_WORKSPACE_ID"), // the default
})
if err != nil {
	log.Fatal(err)
}

integrations, response, err := client.IntegrationsAPI.ListIntegrations(ctx).Execute()
if err != nil {
	log.Fatal(nexra.ParseError(response, err))
}
for _, integration := range integrations {
	fmt.Println(integration.GetName(), integration.GetStatus())
}

// Start a run. An idempotency key is added for you, so a retry never starts a second run.
run, err := client.StartRun(ctx, "cmflow01orders", "sandbox", nexra.StartRunOptions{})
if err != nil {
	log.Fatal(err)
}
detail, response, err := client.RunsAPI.GetRun(ctx, run.GetId()).Execute()
```

Each area of the API is a service on the client: `IntegrationsAPI`,
`FlowsAPI`, `RunsAPI`, `ConnectionsAPI`, `TriggersAPI`, `ReconciliationAPI`,
`ApprovalsAPI`, `EventWebhooksAPI`, `GovernanceAPI`, `LifecycleAPI`,
`SchedulesAPI`, `FailedRecordsAPI`, `TemplatesAPI`, `CatalogueAPI`,
`EndUsersAPI`, `RecordsAPI`, `PensionsAPI`, `ResearchAPI`, `CompanyDataAPI`
and `MCPAPI`. Models are in `nexraapi`.

## Errors

Pass the `(response, err)` from `Execute()` to `nexra.ParseError`. A refusal
becomes an `*nexra.APIError` with `Status`, `Code`, `RequestID` and Nexra's
`Message`, and matches one of these with `errors.Is`:

| Status | Error |
| --- | --- |
| 400, 422 | `nexra.ErrBadRequest` |
| 401 | `nexra.ErrAuthentication` |
| 403 | `nexra.ErrPermission` |
| 404 | `nexra.ErrNotFound` |
| 409 | `nexra.ErrConflict` |
| 429 | `nexra.ErrRateLimited` (with `RetryAfter`) |
| 5xx | `nexra.ErrServer` |

```go
flow, response, err := client.FlowsAPI.GetFlow(ctx, "cmflow01orders").Execute()
if err := nexra.ParseError(response, err); errors.Is(err, nexra.ErrNotFound) {
	fmt.Println("No such flow")
} else if err != nil {
	var apiError *nexra.APIError
	if errors.As(err, &apiError) {
		log.Fatalf("%s (request %s)", apiError.Message, apiError.RequestID)
	}
	log.Fatal(err)
}
```

`client.StartRun` returns `*APIError` already.

## Retries

Requests are retried up to 3 times with exponential backoff and jitter on
network errors and on 408, 425, 429, 500, 502, 503 and 504, honouring
`Retry-After`. A POST or PATCH is only retried when Nexra did not act on it
(429) or when its body has an `idempotencyKey`. Change this with
`Options{Retry: &nexra.RetryOptions{MaxRetries: 5, BaseDelay: 500 * time.Millisecond, MaxDelay: 20 * time.Second}}`
or turn it off with `Options{DisableRetries: true}`. Waits stop when the
request's context ends.

## Cursor pages

Most lists return the newest items as a slice. Governance lists return
`items` and `nextCursor`; `nexra.Paginate` follows the cursor and gives a
Go 1.23 iterator:

```go
entries := nexra.Paginate(ctx, func(ctx context.Context, cursor string) (nexra.Page[nexraapi.EvidenceEntry], error) {
	request := client.GovernanceAPI.ListEvidence(ctx)
	if cursor != "" {
		request = request.Cursor(cursor)
	}
	page, response, err := request.Execute()
	if err != nil {
		return nexra.Page[nexraapi.EvidenceEntry]{}, nexra.ParseError(response, err)
	}
	return nexra.Page[nexraapi.EvidenceEntry]{Items: page.Items, NextCursor: page.GetNextCursor()}, nil
})
for entry, err := range entries {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(entry.GetSequence(), entry.GetEventType())
}
```

## Verify event webhooks

Nexra signs each event delivery with your endpoint's signing secret. Verify
against the raw body before parsing it:

```go
http.HandleFunc("/nexra/events", func(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	event, err := nexra.ConstructEvent(raw, r.Header.Get(nexra.SignatureHeader), secret, nexra.VerifyOptions{})
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	// De-duplicate on event.ID, queue the work, answer quickly.
	w.WriteHeader(http.StatusNoContent)
})
```

Signatures older than 5 minutes are refused. Answer with a 2xx within 8
seconds. Decode the whole body into a `nexraapi.Event*` model (for example
`nexraapi.EventFlowRunFailed`) for typed fields. See
`examples/webhook-receiver`.

## Send events to a webhook trigger

```go
body := []byte(`{"orderId":"1001"}`)
headers, err := nexra.SignTriggerRequest(body, triggerSecret, "order-1001-0001", time.Now())
```

## Versioning

This module follows semantic versioning from v1.0.0. The API it is built from
is dated (`2026-10-01`); Nexra adds operations and optional fields without a
new date, and a minor SDK release picks them up. A breaking API change gets a
new dated version and a new major version (with a `/v2` module path).

## Reference

- Developer portal: https://developer.amakoragroup.com
- API reference: https://developer.amakoragroup.com/docs/platform-api
- OpenAPI document: https://api.nexra.amakoragroup.com/api/public/openapi.json

## For maintainers

`nexraapi/` is generated by openapi-generator 7.25.0 (`go`) from
`openapi/nexra-platform.json` at the repository root. Everything else is
written by hand. This directory is developed inside the Nexra backend
repository and published by copying it to the public module repository
`github.com/TochukwuAchebe/nexra-go` and tagging it (see
`docs/sdk/releasing.md`).

```bash
bash scripts/sdk-codegen/generate.sh go   # after the API document changes (needs Node.js and Java 11+)
bash scripts/sdk-codegen/check.sh go      # generated code is current
go vet ./... && go test ./...
bash scripts/sdk-release/go.sh            # dry run; --publish to push and tag
```
