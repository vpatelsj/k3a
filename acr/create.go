package acr

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
)

type CreateArgs struct {
	SubscriptionID string
	Cluster        string
	ACRName        string
	Location       string
	SKU            string
}

// Create creates an Azure Container Registry
func Create(args CreateArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Create ACR
	if err := createACR(ctx, args, cred); err != nil {
		return fmt.Errorf("failed to create ACR: %w", err)
	}

	return nil
}

// createACR creates an Azure Container Registry
func createACR(ctx context.Context, args CreateArgs, cred *azidentity.DefaultAzureCredential) error {
	registriesClient, err := armcontainerregistry.NewRegistriesClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create registries client: %w", err)
	}

	// Check if ACR already exists
	existing, err := registriesClient.Get(ctx, args.Cluster, args.ACRName, nil)
	if err == nil {
		fmt.Printf("ACR '%s' already exists in resource group '%s'\n", args.ACRName, args.Cluster)
		if existing.Registry.Properties != nil && existing.Registry.Properties.LoginServer != nil {
			fmt.Printf("Login server: %s\n", *existing.Registry.Properties.LoginServer)
		}
		return nil
	}

	registry := armcontainerregistry.Registry{
		Location: to.Ptr(args.Location),
		SKU: &armcontainerregistry.SKU{
			Name: (*armcontainerregistry.SKUName)(to.Ptr(args.SKU)),
		},
		Properties: &armcontainerregistry.RegistryProperties{
			AdminUserEnabled: to.Ptr(false), // Disabled since we'll use imagePullSecrets
		},
		Tags: map[string]*string{
			"k3a":     to.Ptr("acr"),
			"cluster": to.Ptr(args.Cluster),
		},
	}

	poller, err := registriesClient.BeginCreate(ctx, args.Cluster, args.ACRName, registry, nil)
	if err != nil {
		return fmt.Errorf("failed to begin ACR creation: %w", err)
	}

	fmt.Printf("ACR creation started, waiting for completion (max 8 minutes)...\n")

	// Use custom polling with timeout and progress reporting
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Minute)
	defer pollCancel()

	pollCount := 0
	// Poll with shorter intervals and timeout handling
	for {
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("ACR creation timed out after 8 minutes - check Azure portal to verify creation status")
		default:
			// Check if operation is complete
			if poller.Done() {
				_, err = poller.Result(ctx)
				if err != nil {
					return fmt.Errorf("ACR creation failed: %w", err)
				}
				fmt.Printf("ACR provisioning completed successfully.\n")
				return nil
			}

			pollCount++
			if pollCount%4 == 0 { // Print progress every minute (4 * 15s = 60s)
				fmt.Printf("Still creating ACR (elapsed: %d minutes)...\n", pollCount/4)
			}

			// Wait before next poll
			time.Sleep(15 * time.Second)
		}
	}
}
