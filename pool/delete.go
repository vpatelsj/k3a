package pool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/jwilder/k3a/pkg/spinner"
	kstrings "github.com/jwilder/k3a/pkg/strings"
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

	// Compute clusterHash for consistent LB naming
	clusterHash := kstrings.UniqueString(cluster)
	// Delete the backend pool from the load balancer
	lbName := strings.ToLower("k3alb" + clusterHash)
	backendPoolName := fmt.Sprintf("k3a-%s-backend-pool", poolName)
	backendPoolsClient, err := armnetwork.NewLoadBalancerBackendAddressPoolsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create backend address pools client: %w", err)
	}
	deletePoller, err := backendPoolsClient.BeginDelete(ctx, cluster, lbName, backendPoolName, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.ErrorCode == "ResourceNotFound" {
			fmt.Printf("Backend pool '%s' or load balancer '%s' not found, skipping deletion.\n", backendPoolName, lbName)
		} else {
			return fmt.Errorf("failed to start backend pool deletion: %w", err)
		}
	} else {
		_, err = deletePoller.PollUntilDone(ctx, nil)
		if err != nil {
			var respErr *azcore.ResponseError
			if errors.As(err, &respErr) && respErr.ErrorCode == "ResourceNotFound" {
				fmt.Printf("Backend pool '%s' or load balancer '%s' not found, skipping deletion.\n", backendPoolName, lbName)
			} else {
				return fmt.Errorf("failed to delete backend pool: %w", err)
			}
		}
	}

	fmt.Printf("Pool '%s' and backend pool '%s' deleted successfully in cluster '%s'.\n", poolName, backendPoolName, cluster)
	return nil
}
