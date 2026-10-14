// Lists integrations and, given a flow id, starts a sandbox run and reads it back.
//
//	NEXRA_TOKEN=... NEXRA_WORKSPACE_ID=... go run ./examples/quickstart <flow_id>
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	nexra "github.com/TochukwuAchebe/nexra-go"
)

func main() {
	ctx := context.Background()
	client, err := nexra.NewClient(nexra.Options{})
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

	if len(os.Args) < 2 {
		return
	}
	run, err := client.StartRun(ctx, os.Args[1], "sandbox", nexra.StartRunOptions{})
	if errors.Is(err, nexra.ErrNotFound) {
		log.Fatalf("No flow %s in this workspace", os.Args[1])
	} else if err != nil {
		log.Fatal(err)
	}
	detail, response, err := client.RunsAPI.GetRun(ctx, run.GetId()).Execute()
	if err != nil {
		log.Fatal(nexra.ParseError(response, err))
	}
	fmt.Println("Run", detail.GetId(), detail.GetStatus(), detail.GetRecordsSucceeded(), "succeeded")
}
