package acr

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/rodaine/table"
)

type ListArgs struct {
	SubscriptionID string
	Cluster        string
}

// List lists all ACR instances for a cluster
func List(args ListArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to create Azure credential: %w", err)
	}

	registriesClient, err := armcontainerregistry.NewRegistriesClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create registries client: %w", err)
	}

	pager := registriesClient.NewListByResourceGroupPager(args.Cluster, nil)

	tbl := table.New("NAME", "LOCATION", "SKU", "LOGIN_SERVER", "STATUS")

	hasResults := false
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to get ACR list: %w", err)
		}

		for _, registry := range page.Value {
			if registry.Name == nil {
				continue
			}

			// Check if this ACR belongs to the k3a cluster
			if registry.Tags != nil && registry.Tags["k3a"] != nil && registry.Tags["cluster"] != nil && *registry.Tags["cluster"] == args.Cluster {
				hasResults = true
				location := ""
				if registry.Location != nil {
					location = *registry.Location
				}

				sku := ""
				if registry.SKU != nil && registry.SKU.Name != nil {
					sku = string(*registry.SKU.Name)
				}

				loginServer := ""
				if registry.Properties != nil && registry.Properties.LoginServer != nil {
					loginServer = *registry.Properties.LoginServer
				}

				status := ""
				if registry.Properties != nil && registry.Properties.ProvisioningState != nil {
					status = string(*registry.Properties.ProvisioningState)
				}

				tbl.AddRow(*registry.Name, location, sku, loginServer, status)
			}
		}
	}

	if !hasResults {
		fmt.Printf("No ACR instances found for cluster '%s'\n", args.Cluster)
		return nil
	}

	tbl.Print()
	return nil
}
