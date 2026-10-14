package nexra_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	nexra "github.com/TochukwuAchebe/nexra-go"
	"github.com/TochukwuAchebe/nexra-go/nexraapi"
)

// Compiled with the tests so the README examples stay correct. Not run.

func ExampleNewClient() {
	ctx := context.Background()
	client, err := nexra.NewClient(nexra.Options{}) // NEXRA_TOKEN and NEXRA_WORKSPACE_ID
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
	_, response, err = client.FlowsAPI.GetFlow(ctx, "cmflow01orders").Execute()
	if err := nexra.ParseError(response, err); errors.Is(err, nexra.ErrNotFound) {
		fmt.Println("No such flow")
	}
}

func ExamplePaginate() {
	ctx := context.Background()
	client, _ := nexra.NewClient(nexra.Options{})
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
}
