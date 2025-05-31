package rules

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/rodaine/table"
)

type ListArgs struct {
	SubscriptionID string
	ResourceGroup  string
	NSGName        string
	All            bool // If true, show default rules too
}

func List(args ListArgs) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := armnetwork.NewSecurityGroupsClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return err
	}
	nsg, err := client.Get(ctx, args.ResourceGroup, args.NSGName, nil)
	if err != nil {
		return err
	}
	rulesTable := table.New("NAME", "PRIORITY", "DIRECTION", "ACCESS", "PROTOCOL", "SRC", "SRC PORT", "DEST", "DEST PORT")
	if nsg.Properties != nil {
		// List custom rules (inbound and outbound)
		if nsg.Properties.SecurityRules != nil {
			for _, rule := range nsg.Properties.SecurityRules {
				props := rule.Properties
				if props == nil {
					continue
				}
				name := safeString(rule.Name)
				priority := safeInt32(props.Priority)
				direction := *props.Direction
				access := *props.Access
				protocol := *props.Protocol
				src := safeString(props.SourceAddressPrefix)
				srcPort := safeString(props.SourcePortRange)
				dest := safeString(props.DestinationAddressPrefix)
				destPort := safeString(props.DestinationPortRange)
				rulesTable.AddRow(
					name,
					priority,
					direction,
					access,
					protocol,
					src,
					srcPort,
					dest,
					destPort,
				)
			}
		}
		// List default rules (inbound and outbound) if All is true
		if args.All && nsg.Properties.DefaultSecurityRules != nil {
			for _, rule := range nsg.Properties.DefaultSecurityRules {
				props := rule.Properties
				if props == nil {
					continue
				}
				name := safeString(rule.Name)
				priority := safeInt32(props.Priority)
				direction := *props.Direction
				access := *props.Access
				protocol := *props.Protocol
				src := safeString(props.SourceAddressPrefix)
				srcPort := safeString(props.SourcePortRange)
				dest := safeString(props.DestinationAddressPrefix)
				destPort := safeString(props.DestinationPortRange)
				rulesTable.AddRow(
					name,
					priority,
					direction,
					access,
					protocol,
					src,
					srcPort,
					dest,
					destPort,
				)
			}
		}
	}
	rulesTable.Print()
	return nil
}

// safeString returns the string value or "-" if nil
func safeString(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

// safeInt32 returns the int32 value or "-" if nil
func safeInt32(i *int32) interface{} {
	if i == nil {
		return "-"
	}
	return *i
}
