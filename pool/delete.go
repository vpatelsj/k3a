package pool

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/jwilder/k3a/pkg/spinner"
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
	done := spinner.Spinner("Deleting VMSS...")
	poller, err := vmssClient.BeginDelete(ctx, cluster, vmssName, nil)
	if err != nil {
		done()
		return fmt.Errorf("failed to start VMSS deletion: %w", err)
	}
	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		done()
		return fmt.Errorf("failed to delete VMSS: %w", err)
	}
	done()
	fmt.Printf("Pool '%s' deleted successfully in cluster '%s'.\n", poolName, cluster)
	return nil
}
