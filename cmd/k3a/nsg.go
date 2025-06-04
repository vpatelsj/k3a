package main

import (
	"os"

	"github.com/jwilder/k3a/nsg"
	"github.com/spf13/cobra"
)

var nsgCmd = &cobra.Command{
	Use:   "nsg",
	Short: "Manage Azure Network Security Groups",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var nsgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Network Security Groups",
	Run: func(cmd *cobra.Command, args []string) {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		cluster, _ := cmd.Flags().GetString("cluster")
		if subscriptionID == "" {
			cmd.PrintErrln("--subscription is required")
			os.Exit(1)
		}
		if cluster == "" {
			cmd.PrintErrln("--cluster is required")
			os.Exit(1)
		}
		err := nsg.List(subscriptionID, cluster)
		if err != nil {
			cmd.PrintErrln("Error listing NSGs:", err)
			os.Exit(1)
		}
	},
}

func init() {
	nsgListCmd.Flags().String("cluster", os.Getenv("K3A_CLUSTER"), "Cluster (Azure Resource Group, or set AZURE_RESOURCE_GROUP)")
	nsgCmd.AddCommand(nsgListCmd)
	rootCmd.AddCommand(nsgCmd)
}
