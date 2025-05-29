package pool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/jwilder/k3a/pkg/bicep"
	kstrings "github.com/jwilder/k3a/pkg/strings"
	"github.com/urfave/cli/v2"
)

type CreatePoolArgs struct {
	SubscriptionID string
	Cluster        string
	Location       string
	Role           string
	Name           string
	SSHKeyPath     string
	InstanceCount  int
}

func Create(args CreatePoolArgs) error {
	subscriptionID := args.SubscriptionID
	cluster := args.Cluster
	location := args.Location
	bicepFile := "pool.bicep"
	role := args.Role
	if role != "" && role != "control-plane" && role != "worker" {
		return fmt.Errorf("invalid role: %s (must be 'control-plane' or 'worker')", role)
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
	vmssName := args.Name + "-vmss"
	vmss, err := vmssClient.Get(ctx, cluster, vmssName, nil)
	if err == nil && vmss.Name != nil {
		if vmss.Tags != nil {
			if v, ok := vmss.Tags["k3a"]; ok && v != nil {
				existingRole := *v
				if existingRole != role {
					return fmt.Errorf("VMSS '%s' already exists with a different role: %s", vmssName, existingRole)
				}
			}
		}
	}

	if role == "control-plane" {
		pager := vmssClient.NewListPager(cluster, nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed to list VMSS: %w", err)
			}
			for _, existingVMSS := range page.Value {
				if existingVMSS.Name != nil && *existingVMSS.Name != vmssName && existingVMSS.Tags != nil {
					if v, ok := existingVMSS.Tags["k3a"]; ok && v != nil && *v == "control-plane" {
						return fmt.Errorf("A VMSS with role 'control-plane' already exists: %s", *existingVMSS.Name)
					}
				}
			}
		}
	}

	sshKeyPath := args.SSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = os.ExpandEnv("$HOME/.ssh/id_rsa.pub")
	}
	sshKeyBytes, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH public key from %s: %w", sshKeyPath, err)
	}
	sshKey := string(sshKeyBytes)

	bicepTmpl, err := bicep.CompileBicep(bicepFile)
	if err != nil {
		return fmt.Errorf("failed to compile bicep file: %w", err)
	}

	var b map[string]interface{}
	if err := json.Unmarshal(bicepTmpl, &b); err != nil {
		return fmt.Errorf("Failed to parse template JSON: %v", err)
	}

	clusterHash := kstrings.UniqueString(cluster)

	publicIPClient, err := armnetwork.NewPublicIPAddressesClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create public IP client: %w", err)
	}
	publicIPName := fmt.Sprintf("k3alb%s-publicip", clusterHash)
	publicIPResp, err := publicIPClient.Get(ctx, cluster, publicIPName, nil)
	if err != nil {
		return fmt.Errorf("failed to get public IP '%s': %w", publicIPName, err)
	}
	externalIP := ""
	if publicIPResp.PublicIPAddress.Properties != nil && publicIPResp.PublicIPAddress.Properties.IPAddress != nil {
		externalIP = *publicIPResp.PublicIPAddress.Properties.IPAddress
	}
	if externalIP == "" {
		return fmt.Errorf("could not determine external IP for public IP resource '%s'", publicIPName)
	}

	cloudInitBytes, err := os.ReadFile("modules/cloud-init.yaml")
	if err != nil {
		return fmt.Errorf("failed to read cloud-init.yaml: %w", err)
	}

	keyVaultName := fmt.Sprintf("k3akv%s", clusterHash)
	posgresName := fmt.Sprintf("k3apg%s", clusterHash)
	storageAccountName := fmt.Sprintf("k3astorage%s", clusterHash)
	tmplData := map[string]string{
		"PostgresURL":        "",
		"KeyVaultName":       keyVaultName,
		"PostgresName":       posgresName,
		"PostgresSuffix":     "postgres.database.azure.com",
		"Role":               role,
		"StorageAccountName": storageAccountName,
		"ResourceGroup":      cluster,
		"ExternalIP":         externalIP,
	}

	tmpl, err := template.New("cloud-init").Parse(string(cloudInitBytes))
	if err != nil {
		return fmt.Errorf("failed to parse cloud-init template: %w", err)
	}
	var renderedCloudInit bytes.Buffer
	if err := tmpl.Execute(&renderedCloudInit, tmplData); err != nil {
		return fmt.Errorf("failed to render cloud-init template: %w", err)
	}

	customDataB64 := base64.StdEncoding.EncodeToString(renderedCloudInit.Bytes())

	isControlPlane := false
	if role == "control-plane" {
		isControlPlane = true
	}

	instanceCount := args.InstanceCount

	parameters := map[string]interface{}{
		"prefix":         map[string]interface{}{"value": "k3a"},
		"location":       map[string]interface{}{"value": location},
		"poolName":       map[string]interface{}{"value": args.Name},
		"isControlPlane": map[string]interface{}{"value": isControlPlane},
		"role":           map[string]interface{}{"value": role},
		"sshPublicKey":   map[string]interface{}{"value": sshKey},
		"clusterHash":    map[string]interface{}{"value": clusterHash},
		"customData":     map[string]interface{}{"value": customDataB64},
		"instanceCount":  map[string]interface{}{"value": instanceCount},
	}

	cred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	ctx = context.Background()
	deploymentsClient, err := armresources.NewDeploymentsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create deployments client: %w", err)
	}

	deploymentName := fmt.Sprintf("pool-deploy-%d", time.Now().Unix())

	poller, err := deploymentsClient.BeginCreateOrUpdate(
		ctx,
		cluster,
		deploymentName,
		armresources.Deployment{
			Properties: &armresources.DeploymentProperties{
				Template:   b,
				Parameters: parameters,
				Mode:       to.Ptr(armresources.DeploymentModeIncremental),
			},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to start pool deployment: %w", err)
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("pool deployment failed: %w", err)
	}

	fmt.Printf("Pool deployment succeeded: %v\n", *resp.ID)
	return nil
}

func createPoolAction(c *cli.Context) error {
	args := CreatePoolArgs{
		SubscriptionID: c.String("subscription"),
		Cluster:        c.String("cluster"),
		Location:       c.String("region"),
		Role:           c.String("role"),
		Name:           c.String("name"),
		SSHKeyPath:     c.String("ssh-key"),
		InstanceCount:  c.Int("instance-count"),
	}
	return Create(args)
}
