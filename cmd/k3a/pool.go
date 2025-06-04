package main

import (
	"fmt"
	"os"

	"github.com/jwilder/k3a/pool"
	"github.com/spf13/cobra"
)

var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage VMSS pools (list, create, delete, scale)",
}

var listPoolsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Virtual Machine Scale Sets (VMSS) in the specified resource group.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		cluster, _ := cmd.Flags().GetString("cluster")
		if cluster == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}

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
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		cluster, _ := cmd.Flags().GetString("cluster")
		if cluster == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		location, _ := cmd.Flags().GetString("region")
		role, _ := cmd.Flags().GetString("role")
		name, _ := cmd.Flags().GetString("name")
		sshKeyPath, _ := cmd.Flags().GetString("ssh-key")
		instanceCount, _ := cmd.Flags().GetInt("instance-count")
		k8sVersion, _ := cmd.Flags().GetString("k8s-version")
		// matched, err := regexp.MatchString(`^v\\d+\\.\\d+\\.\\d+`, k8sVersion)
		// if err != nil {
		// 	return err
		// }
		// if !matched {
		// 	return fmt.Errorf("invalid k3s version format: must match v<major>.<minor>.<patch> (e.g. v1.28.5)")
		// }

		return pool.Create(pool.CreatePoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
			Location:       location,
			Role:           role,
			Name:           name,
			SSHKeyPath:     sshKeyPath,
			InstanceCount:  instanceCount,
			K8sVersion:     k8sVersion,
		})
	},
}

var deletePoolCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a VMSS pool.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		cluster, _ := cmd.Flags().GetString("cluster")
		if cluster == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}

		name, _ := cmd.Flags().GetString("name")
		return pool.Delete(pool.DeletePoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
			Name:           name,
		})
	},
}

var scalePoolCmd = &cobra.Command{
	Use:   "scale",
	Short: "Scale a VMSS pool to the desired number of instances.",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		cluster, _ := cmd.Flags().GetString("cluster")
		if cluster == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			return fmt.Errorf("--name flag is required")
		}
		instanceCount, _ := cmd.Flags().GetInt("instance-count")
		if instanceCount < 1 {
			return fmt.Errorf("--instance-count must be greater than 0")
		}
		return pool.Scale(pool.ScalePoolArgs{
			SubscriptionID: subscriptionID,
			Cluster:        cluster,
			Name:           name,
			InstanceCount:  instanceCount,
		})
	},
}

func init() {
	clusterDefault := ""
	if v := os.Getenv("K3A_CLUSTER"); v != "" {
		clusterDefault = v
	}
	// Pool list flags
	listPoolsCmd.Flags().String("cluster", clusterDefault, "Cluster name (or set K3A_CLUSTER) (required)")

	// Pool create flags
	createPoolCmd.Flags().String("cluster", clusterDefault, "Cluster name (or set K3A_CLUSTER) (required)")
	createPoolCmd.Flags().String("name", "", "Name of the node pool (required)")
	createPoolCmd.Flags().String("role", "control-plane", "Role of the node pool (control-plane or worker)")
	createPoolCmd.Flags().String("region", "canadacentral", "Azure region for the pool")
	createPoolCmd.Flags().Int("instance-count", 1, "Number of VMSS instances")
	createPoolCmd.Flags().String("ssh-key", os.ExpandEnv("$HOME/.ssh/id_rsa.pub"), "Path to the SSH public key file")
	createPoolCmd.Flags().String("k8s-version", "v1.33.1", "Kubernetes (k3s) version (e.g. v1.33.1)")

	_ = createPoolCmd.MarkFlagRequired("name")
	_ = createPoolCmd.MarkFlagRequired("role")

	// Pool delete flags
	deletePoolCmd.Flags().String("cluster", clusterDefault, "Cluster name (or set K3A_CLUSTER) (required)")
	deletePoolCmd.Flags().String("name", "", "Name of the node pool (required)")
	_ = deletePoolCmd.MarkFlagRequired("name")

	// Pool scale flags
	scalePoolCmd.Flags().String("cluster", clusterDefault, "Cluster name (or set K3A_CLUSTER) (required)")
	scalePoolCmd.Flags().String("name", "", "Name of the node pool (required)")
	scalePoolCmd.Flags().Int("instance-count", 1, "Number of VMSS instances (required)")
	_ = scalePoolCmd.MarkFlagRequired("name")
	_ = scalePoolCmd.MarkFlagRequired("instance-count")

	poolCmd.AddCommand(listPoolsCmd, createPoolCmd, deletePoolCmd, scalePoolCmd)
	rootCmd.AddCommand(poolCmd)
}
