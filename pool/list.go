package pool

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/rodaine/table"
)

type ListPoolArgs struct {
	SubscriptionID string
	Cluster        string
}

func List(args ListPoolArgs) error {
	subscriptionID := args.SubscriptionID
	cluster := args.Cluster

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	ctx := context.Background()
	vmssClient, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create VMSS client: %w", err)
	}
	pager := vmssClient.NewListPager(cluster, nil)

	tbl := table.New("CLUSTER", "NAME", "ROLE", "LOCATION", "SKU", "SIZE")
	found := false
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to get VMSS: %w", err)
		}
		for _, vmss := range page.Value {
			if vmss.Name != nil {
				poolName := strings.TrimSuffix(*vmss.Name, "-vmss")
				location := ""
				if vmss.Location != nil {
					location = *vmss.Location
				}
				size := "-"
				if vmss.SKU != nil && vmss.SKU.Capacity != nil {
					size = fmt.Sprintf("%d", *vmss.SKU.Capacity)
				}
				vmType := "-"
				if vmss.SKU != nil && vmss.SKU.Name != nil {
					vmType = *vmss.SKU.Name
				}
				role := "-"
				if vmss.Tags != nil {
					if v, ok := vmss.Tags["k3a"]; ok && v != nil {
						role = *v
					}
				}
				tbl.AddRow(cluster, poolName, role, location, vmType, size)
				found = true
			}
		}
	}
	tbl.Print()
	if !found {
		fmt.Println("No pools (VMSS) found in resource group.")
	}
	return nil
}
