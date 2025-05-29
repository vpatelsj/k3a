package pool

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
)

type DeletePoolArgs struct {
	SubscriptionID string
	Cluster        string
	Name           string
}

func Delete(args DeletePoolArgs) error {
	subscriptionID := args.SubscriptionID
	cluster := args.Cluster
	poolName := args.Name
	if poolName == "" {
		return fmt.Errorf("--name flag is required to delete a pool")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	ctx := context.Background()
	vmssClient, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create VMSS client: %w", err)
	}
	vmssName := poolName + "-vmss"
	poller, err := vmssClient.BeginDelete(ctx, cluster, vmssName, nil)
	if err != nil {
		return fmt.Errorf("failed to start VMSS deletion: %w", err)
	}
	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete VMSS: %w", err)
	}
	fmt.Printf("Pool '%s' deleted successfully in cluster '%s'.\n", poolName, cluster)
	return nil
}
