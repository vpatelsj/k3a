package main

import (
	"os"

	"github.com/jwilder/k3a/pool"
	"github.com/spf13/cobra"
)

var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage VMSS pools (list, create, delete)",
}

var listPoolsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Virtual Machine Scale Sets (VMSS) in the specified resource group.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		cluster, _ := cmd.Flags().GetString("cluster")
		return pool.List(pool.ListPoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
		})
	},
}

var createPoolCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new VMSS pool.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		cluster, _ := cmd.Flags().GetString("cluster")
		location, _ := cmd.Flags().GetString("region")
		role, _ := cmd.Flags().GetString("role")
		name, _ := cmd.Flags().GetString("name")
		sshKeyPath, _ := cmd.Flags().GetString("ssh-key")
		instanceCount, _ := cmd.Flags().GetInt("instance-count")
		return pool.Create(pool.CreatePoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
			Location:       location,
			Role:           role,
			Name:           name,
			SSHKeyPath:     sshKeyPath,
			InstanceCount:  instanceCount,
		})
	},
}

var deletePoolCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a VMSS pool.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		cluster, _ := cmd.Flags().GetString("cluster")
		name, _ := cmd.Flags().GetString("name")
		return pool.Delete(pool.DeletePoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
			Name:           name,
		})
	},
}

func init() {
	// Pool list flags
	listPoolsCmd.Flags().String("cluster", "", "Cluster name (required)")
	_ = listPoolsCmd.MarkFlagRequired("cluster")

	// Pool create flags
	createPoolCmd.Flags().String("cluster", "", "Cluster name (required)")
	createPoolCmd.Flags().String("name", "", "Name of the node pool (required)")
	createPoolCmd.Flags().String("role", "control-plane", "Role of the node pool (control-plane or worker)")
	createPoolCmd.Flags().String("region", "canadacentral", "Azure region for the pool")
	createPoolCmd.Flags().Int("instance-count", 1, "Number of VMSS instances")
	createPoolCmd.Flags().String("ssh-key", os.ExpandEnv("$HOME/.ssh/id_rsa.pub"), "Path to the SSH public key file")
	_ = createPoolCmd.MarkFlagRequired("cluster")
	_ = createPoolCmd.MarkFlagRequired("name")
	_ = createPoolCmd.MarkFlagRequired("role")

	// Pool delete flags
	deletePoolCmd.Flags().String("cluster", "", "Cluster name (required)")
	deletePoolCmd.Flags().String("name", "", "Name of the node pool (required)")
	_ = deletePoolCmd.MarkFlagRequired("cluster")
	_ = deletePoolCmd.MarkFlagRequired("name")

	poolCmd.AddCommand(listPoolsCmd, createPoolCmd, deletePoolCmd)
	rootCmd.AddCommand(poolCmd)
}
