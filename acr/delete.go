package acr

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
)

type DeleteArgs struct {
	SubscriptionID string
	Cluster        string
	ACRName        string
}

// Delete deletes an Azure Container Registry
func Delete(args DeleteArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to create Azure credential: %w", err)
	}

	registriesClient, err := armcontainerregistry.NewRegistriesClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create registries client: %w", err)
	}

	poller, err := registriesClient.BeginDelete(ctx, args.Cluster, args.ACRName, nil)
	if err != nil {
		return fmt.Errorf("failed to begin ACR deletion: %w", err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to complete ACR deletion: %w", err)
	}

	return nil
}
