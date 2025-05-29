package main

import (
	"github.com/jwilder/k3a/loadbalancer"
	"github.com/jwilder/k3a/loadbalancer/rule"
	"github.com/spf13/cobra"
)

var loadBalancerCmd = &cobra.Command{
	Use:     "loadbalancer",
	Aliases: []string{"lb"},
	Short:   "Manage Azure Load Balancers",
}

var listLoadBalancersCmd = &cobra.Command{
	Use:   "list",
	Short: "List load balancers in a resource group",
	RunE: func(cmd *cobra.Command, args []string) error {
		lbCluster, _ := cmd.Flags().GetString("cluster")
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		return loadbalancer.List(loadbalancer.ListLoadBalancerArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  lbCluster,
		})
	},
}

var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage load balancer rules",
}

var ruleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a load balancer rule",
	RunE: func(cmd *cobra.Command, args []string) error {
		lbCluster, _ := cmd.Flags().GetString("cluster")
		lbName, _ := cmd.Flags().GetString("name")
		lbRuleName, _ := cmd.Flags().GetString("rule-name")
		lbFrontendPort, _ := cmd.Flags().GetInt("frontend-port")
		lbBackendPort, _ := cmd.Flags().GetInt("backend-port")
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		return rule.Create(rule.CreateRuleArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  lbCluster,
			LBName:         lbName,
			RuleName:       lbRuleName,
			FrontendPort:   lbFrontendPort,
			BackendPort:    lbBackendPort,
		})
	},
}

var ruleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List load balancer rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		lbCluster, _ := cmd.Flags().GetString("cluster")
		lbName, _ := cmd.Flags().GetString("name")
		return rule.List(rule.ListRuleArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  lbCluster,
			LBName:         lbName,
		})
	},
}

var ruleDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a load balancer rule",
	RunE: func(cmd *cobra.Command, args []string) error {
		lbCluster, _ := cmd.Flags().GetString("cluster")
		lbName, _ := cmd.Flags().GetString("name")
		lbRuleName, _ := cmd.Flags().GetString("rule-name")
		return rule.Delete(rule.DeleteRuleArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  lbCluster,
			LBName:         lbName,
			RuleName:       lbRuleName,
		})
	},
}

func init() {
	// List load balancers flags
	listLoadBalancersCmd.Flags().String("cluster", "", "Azure resource group name (required)")
	_ = listLoadBalancersCmd.MarkFlagRequired("cluster")

	// Rule create flags
	ruleCreateCmd.Flags().String("cluster", "", "Azure resource group name (required)")
	ruleCreateCmd.Flags().String("name", "", "Load balancer name (required)")
	ruleCreateCmd.Flags().String("rule-name", "", "Load balancer rule name (required)")
	ruleCreateCmd.Flags().Int("frontend-port", 0, "Frontend port (required)")
	ruleCreateCmd.Flags().Int("backend-port", 0, "Backend port (required)")
	_ = ruleCreateCmd.MarkFlagRequired("cluster")
	_ = ruleCreateCmd.MarkFlagRequired("name")
	_ = ruleCreateCmd.MarkFlagRequired("rule-name")
	_ = ruleCreateCmd.MarkFlagRequired("frontend-port")
	_ = ruleCreateCmd.MarkFlagRequired("backend-port")

	// Rule list flags
	ruleListCmd.Flags().String("cluster", "", "Azure resource group name (required)")
	ruleListCmd.Flags().String("name", "", "Load balancer name (required)")
	_ = ruleListCmd.MarkFlagRequired("cluster")
	_ = ruleListCmd.MarkFlagRequired("name")

	// Rule delete flags
	ruleDeleteCmd.Flags().String("cluster", "", "Azure resource group name (required)")
	ruleDeleteCmd.Flags().String("name", "", "Load balancer name (required)")
	ruleDeleteCmd.Flags().String("rule-name", "", "Load balancer rule name (required)")
	_ = ruleDeleteCmd.MarkFlagRequired("cluster")
	_ = ruleDeleteCmd.MarkFlagRequired("name")
	_ = ruleDeleteCmd.MarkFlagRequired("rule-name")

	// Add subcommands
	ruleCmd.AddCommand(ruleCreateCmd, ruleListCmd, ruleDeleteCmd)
	loadBalancerCmd.AddCommand(ruleCmd)
	loadBalancerCmd.AddCommand(listLoadBalancersCmd)
	rootCmd.AddCommand(loadBalancerCmd)
}
