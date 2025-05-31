package rules

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

type AddRuleArgs struct {
	SubscriptionID  string
	ResourceGroup   string
	NSGName         string
	RuleName        string
	Priority        int32
	Direction       string // "Inbound" or "Outbound"
	Access          string // "Allow" or "Deny"
	Protocol        string // "Tcp", "Udp", "*"
	Source          string
	SourcePort      string
	Destination     string
	DestinationPort string
}

func AddRule(args AddRuleArgs) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := armnetwork.NewSecurityRulesClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return err
	}

	ruleParams := armnetwork.SecurityRule{
		Name: &args.RuleName,
		Properties: &armnetwork.SecurityRulePropertiesFormat{
			Priority:                 &args.Priority,
			Direction:                to.Ptr(armnetwork.SecurityRuleDirection(args.Direction)),
			Access:                   to.Ptr(armnetwork.SecurityRuleAccess(args.Access)),
			Protocol:                 to.Ptr(armnetwork.SecurityRuleProtocol(args.Protocol)),
			SourceAddressPrefix:      toPtr(args.Source),
			SourcePortRange:          toPtr(args.SourcePort),
			DestinationAddressPrefix: toPtr(args.Destination),
			DestinationPortRange:     toPtr(args.DestinationPort),
		},
	}

	_, err = client.BeginCreateOrUpdate(ctx, args.ResourceGroup, args.NSGName, args.RuleName, ruleParams, nil)
	if err != nil {
		return fmt.Errorf("failed to add NSG rule: %w", err)
	}
	return nil
}

func toPtr[T any](v T) *T {
	return &v
}
