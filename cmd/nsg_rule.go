package main

import (
	"os"

	"github.com/jwilder/k3a/nsg/rules"
	"github.com/spf13/cobra"
)

var (
	clusterDefault string
	nsgName        string
	allRules       bool

	addRuleName       string
	addRulePriority   int32
	addRuleDirection  string
	addRuleAccess     string
	addRuleProtocol   string
	addRuleSource     string
	addRuleSourcePort string
	addRuleDest       string
	addRuleDestPort   string
)

var nsgRuleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage NSG rules",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var nsgRuleAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a rule to an NSG",
	Run: func(cmd *cobra.Command, args []string) {
		nsgName, _ := cmd.Flags().GetString("nsg-name")
		ruleName, _ := cmd.Flags().GetString("name")
		priority, _ := cmd.Flags().GetInt32("priority")
		direction, _ := cmd.Flags().GetString("direction")
		access, _ := cmd.Flags().GetString("access")
		protocol, _ := cmd.Flags().GetString("protocol")
		source, _ := cmd.Flags().GetString("source")
		sourcePort, _ := cmd.Flags().GetString("source-port")
		dest, _ := cmd.Flags().GetString("dest")
		destPort, _ := cmd.Flags().GetString("dest-port")

		if subscriptionID == "" {
			cmd.Println("Flag --subscription-id is required (or set K3A_SUBSCRIPTION)")
			return
		}
		if clusterDefault == "" {
			cmd.Println("Flag --cluster is required (or set K3A_CLUSTER)")
			return
		}
		if nsgName == "" {
			cmd.Println("Flag --nsg-name is required")
			return
		}
		if ruleName == "" {
			cmd.Println("Flag --name is required")
			return
		}
		if priority == 0 {
			cmd.Println("Flag --priority is required and must be > 0")
			return
		}
		if direction == "" {
			cmd.Println("Flag --direction is required (Inbound or Outbound)")
			return
		}
		if access == "" {
			cmd.Println("Flag --access is required (Allow or Deny)")
			return
		}
		if protocol == "" {
			cmd.Println("Flag --protocol is required (Tcp, Udp, or *)")
			return
		}
		if source == "" {
			cmd.Println("Flag --source is required")
			return
		}
		if sourcePort == "" {
			cmd.Println("Flag --source-port is required")
			return
		}
		if dest == "" {
			cmd.Println("Flag --dest is required")
			return
		}
		if destPort == "" {
			cmd.Println("Flag --dest-port is required")
			return
		}

		addArgs := rules.AddRuleArgs{
			SubscriptionID:  subscriptionID,
			ResourceGroup:   clusterDefault,
			NSGName:         nsgName,
			RuleName:        ruleName,
			Priority:        priority,
			Direction:       direction,
			Access:          access,
			Protocol:        protocol,
			Source:          source,
			SourcePort:      sourcePort,
			Destination:     dest,
			DestinationPort: destPort,
		}

		err := rules.AddRule(addArgs)
		if err != nil {
			cmd.PrintErrln("Error adding NSG rule:", err)
			return
		}
		cmd.Println("NSG rule added successfully.")
	},
}

var nsgRuleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List rules in an NSG",
	Run: func(cmd *cobra.Command, args []string) {
		if subscriptionID == "" {
			cmd.Println("Flag --subscription-id is required (or set K3A_SUBSCRIPTION)")
			return
		}
		if clusterDefault == "" {
			cmd.Println("Flag --cluster is required (or set K3A_CLUSTER)")
			return
		}
		if nsgName == "" {
			cmd.Println("Flag --nsg-name is required")
			return
		}
		listArgs := rules.ListArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  clusterDefault,
			NSGName:        nsgName,
			All:            allRules,
		}
		err := rules.List(listArgs)
		if err != nil {
			cmd.PrintErrln("Error listing NSG rules:", err)
		}
	},
}

var nsgRuleDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a rule from an NSG",
	Run: func(cmd *cobra.Command, args []string) {
		nsgName, _ := cmd.Flags().GetString("nsg-name")
		ruleName, _ := cmd.Flags().GetString("name")

		if subscriptionID == "" {
			cmd.Println("Flag --subscription-id is required (or set K3A_SUBSCRIPTION)")
			return
		}
		if clusterDefault == "" {
			cmd.Println("Flag --cluster is required (or set K3A_CLUSTER)")
			return
		}
		if nsgName == "" {
			cmd.Println("Flag --nsg-name is required")
			return
		}
		if ruleName == "" {
			cmd.Println("Flag --name is required")
			return
		}

		deleteArgs := rules.DeleteRuleArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  clusterDefault,
			NSGName:        nsgName,
			RuleName:       ruleName,
		}
		err := rules.DeleteRule(deleteArgs)
		if err != nil {
			cmd.PrintErrln("Error deleting NSG rule:", err)
			return
		}
		cmd.Println("NSG rule deleted successfully.")
	},
}

func init() {
	nsgCmd.AddCommand(nsgRuleCmd)
	nsgRuleCmd.AddCommand(nsgRuleAddCmd)
	nsgRuleCmd.AddCommand(nsgRuleListCmd)
	nsgRuleCmd.AddCommand(nsgRuleDeleteCmd)

	if v := os.Getenv("K3A_CLUSTER"); v != "" {
		clusterDefault = v
	}

	nsgRuleListCmd.Flags().StringVar(&subscriptionID, "subscription-id", "", "Azure subscription ID")
	nsgRuleListCmd.Flags().StringVar(&clusterDefault, "cluster", clusterDefault, "Cluster name (resource group) (or set K3A_CLUSTER)")
	nsgRuleListCmd.Flags().StringVar(&nsgName, "nsg-name", "", "Azure NSG name")
	nsgRuleListCmd.Flags().BoolVar(&allRules, "all", false, "Show all rules including default rules")

	nsgRuleAddCmd.Flags().String("nsg-name", "", "Azure NSG name")
	nsgRuleAddCmd.Flags().String("name", "", "Rule name (required)")
	nsgRuleAddCmd.Flags().Int32("priority", 0, "Rule priority (required, 100-4096)")
	nsgRuleAddCmd.Flags().String("direction", "Inbound", "Rule direction: Inbound or Outbound (required)")
	nsgRuleAddCmd.Flags().String("access", "Allow", "Rule access: Allow or Deny (required)")
	nsgRuleAddCmd.Flags().String("protocol", "*", "Rule protocol: Tcp, Udp, or * (required)")
	nsgRuleAddCmd.Flags().String("source", "*", "Source address prefix (required)")
	nsgRuleAddCmd.Flags().String("source-port", "*", "Source port range (required)")
	nsgRuleAddCmd.Flags().String("dest", "*", "Destination address prefix (required)")
	nsgRuleAddCmd.Flags().String("dest-port", "*", "Destination port range (required)")

	nsgRuleDeleteCmd.Flags().String("nsg-name", "", "Azure NSG name")
	nsgRuleDeleteCmd.Flags().String("name", "", "Rule name to delete (required)")
}
