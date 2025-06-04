package main

import (
	"fmt"

	"github.com/jwilder/k3a/cluster"
	"github.com/spf13/cobra"
)

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Cluster management commands",
}

var createClusterCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		region, _ := cmd.Flags().GetString("region")
		vnetAddressSpace, _ := cmd.Flags().GetString("vnet-address-space")
		return cluster.Create(cluster.CreateArgs{
			SubscriptionID:   subscriptionID,
			Cluster:          clusterName,
			Location:         region,
			VnetAddressSpace: vnetAddressSpace,
		})
	},
}

var listClustersCmd = &cobra.Command{
	Use:   "list",
	Short: "List cluster deployments",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		return cluster.List(cluster.ListArgs{
			SubscriptionID: subscriptionID,
		})
	},
}

var deleteClusterCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		return cluster.Delete(cluster.DeleteArgs{
			SubscriptionID: subscriptionID,
			Cluster:        clusterName,
		})
	},
}

func init() {
	// Cluster create flags
	createClusterCmd.Flags().String("cluster", "", "Cluster name (or set K3A_CLUSTER) (required)")
	createClusterCmd.Flags().String("region", "", "Azure region for the cluster (e.g., canadacentral) (required)")
	createClusterCmd.Flags().String("vnet-address-space", "10.0.0.0/8", "VNet address space (CIDR, e.g. 10.0.0.0/8)")
	_ = createClusterCmd.MarkFlagRequired("region")

	// Cluster delete flags
	deleteClusterCmd.Flags().String("cluster", "", "Cluster name (required)")
	_ = deleteClusterCmd.MarkFlagRequired("cluster")

	// Add all subcommands to clusterCmd at once
	clusterCmd.AddCommand(createClusterCmd, listClustersCmd, deleteClusterCmd)

	// Register clusterCmd with rootCmd
	rootCmd.AddCommand(clusterCmd)
}
