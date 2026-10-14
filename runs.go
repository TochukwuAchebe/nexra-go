package nexra

import (
	"context"

	"github.com/TochukwuAchebe/nexra-go/nexraapi"
)

// StartRunOptions are the optional fields of a run start.
type StartRunOptions struct {
	// IdempotencyKey defaults to a fresh key, so a retry never starts a second run.
	IdempotencyKey string
	// CorrelationID defaults to a fresh id.
	CorrelationID    string
	TriggerReference string
	InputID          string
	EndUserID        string
	ExternalUserID   string
}

// StartRun starts a run of a published flow in "sandbox" or "production" and
// returns the Execution. Refusals come back as *APIError.
func (c *Client) StartRun(ctx context.Context, flowID, environment string, options StartRunOptions) (*nexraapi.Execution, error) {
	key := options.IdempotencyKey
	if key == "" {
		key = NewIdempotencyKey("run")
	}
	correlation := options.CorrelationID
	if correlation == "" {
		correlation = NewIdempotencyKey("corr")
	}
	body := nexraapi.NewStartRunRequest(environment, key, correlation)
	if options.TriggerReference != "" {
		body.SetTriggerReference(options.TriggerReference)
	}
	if options.InputID != "" {
		body.SetInputId(options.InputID)
	}
	if options.EndUserID != "" {
		body.SetEndUserId(options.EndUserID)
	}
	if options.ExternalUserID != "" {
		body.SetExternalUserId(options.ExternalUserID)
	}
	execution, response, err := c.RunsAPI.StartRun(ctx, flowID).StartRunRequest(*body).Execute()
	if err != nil {
		return nil, ParseError(response, err)
	}
	return execution, nil
}
