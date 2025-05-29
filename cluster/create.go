package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/keyvault/azsecrets"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/postgresql/armpostgresqlflexibleservers"
	"github.com/jwilder/k3a/pkg/bicep"
	"github.com/jwilder/k3a/pkg/spinner"
	kstrings "github.com/jwilder/k3a/pkg/strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

type CreateArgs struct {
	SubscriptionID     string
	Cluster            string
	Location           string
	VnetAddressSpace   string
}

func Create(args CreateArgs) error {
	subscriptionID := args.SubscriptionID
	if subscriptionID == "" {
		return fmt.Errorf("--subscription flag is required")
	}
	cluster := args.Cluster
	location := args.Location
	bicepFile := "main.bicep"
	vnetNamePrefix := "k3a" // Should match your Bicep param

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	ctx := context.Background()

	// Generate the Postgres server name as in Bicep
	clusterHash := kstrings.UniqueString(cluster)
	pgServerName := fmt.Sprintf("%spg%s", vnetNamePrefix, clusterHash)
	pgServerName = strings.ToLower(pgServerName)

	template, err := bicep.CompileBicep(bicepFile)
	if err != nil {
		return fmt.Errorf("failed to compile bicep file: %w", err)
	}

	var b map[string]interface{}
	if err := json.Unmarshal(template, &b); err != nil {
		return fmt.Errorf("failed to parse template JSON: %v", err)
	}

	// Get the calling principal (user/service principal) object ID
	var callingPrincipalID string
	if v := os.Getenv("AZURE_CLIENT_OBJECT_ID"); v != "" {
		callingPrincipalID = v
	} else {
		// fallback: try to get from Azure CLI
		out, err := exec.Command("az", "ad", "signed-in-user", "show", "--query", "id", "-o", "tsv").Output()
		if err != nil {
			return fmt.Errorf("failed to get calling principal id: %w", err)
		}
		callingPrincipalID = strings.TrimSpace(string(out))
	}

	vnetAddressSpace := args.VnetAddressSpace

	// Prepare deployment parameters (example, adjust as needed)
	parameters := map[string]interface{}{
		"resourceGroupName":  map[string]interface{}{"value": cluster},
		"location":           map[string]interface{}{"value": location},
		"vnetAddressSpace":   map[string]interface{}{"value": []string{vnetAddressSpace}},
		"vnetNamePrefix":     map[string]interface{}{"value": "k3a"},
		"adminUsername":      map[string]interface{}{"value": "azureuser"},
		"adminPassword":      map[string]interface{}{"value": "P@ssw0rd123!"},
		"clusterHash":        map[string]interface{}{"value": clusterHash},
		"callingPrincipalId": map[string]interface{}{"value": callingPrincipalID},
	}

	deploymentsClient, err := armresources.NewDeploymentsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create deployments client: %w", err)
	}

	deploymentName := fmt.Sprintf("bicep-deploy-%d", time.Now().Unix())

	// Create deployment
	poller, err := deploymentsClient.BeginCreateOrUpdateAtSubscriptionScope(
		ctx,
		deploymentName,
		armresources.Deployment{
			Properties: &armresources.DeploymentProperties{
				Template:   b,
				Parameters: parameters,
				Mode:       to.Ptr(armresources.DeploymentModeIncremental),
			},
			Location: &location,
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to start deployment: %w", err)
	}

	// Wait for completion with spinner
	stopSpinner := spinner.Spinner("Deploying...")
	resp, err := poller.PollUntilDone(ctx, nil)
	stopSpinner()
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	fmt.Printf("Deployment succeeded: %v\n", *resp.ID)

	// Generate a strong password for Postgres
	postgresPassword, err := kstrings.GeneratePassword(24)
	if err != nil {
		return fmt.Errorf("failed to generate postgres password: %w", err)
	}

	// Reset the Postgres admin password to the generated password
	if err := resetPostgresAdminPassword(ctx, subscriptionID, cluster, pgServerName, "azureuser", postgresPassword); err != nil {
		return fmt.Errorf("failed to reset Postgres admin password: %w", err)
	}

	// Store the password in Key Vault
	keyVaultName := fmt.Sprintf("k3akv%s", clusterHash)
	secretName := "postgres-admin-password"
	if err := storeSecretInKeyVault(ctx, subscriptionID, keyVaultName, secretName, postgresPassword); err != nil {
		return fmt.Errorf("failed to store secret in Key Vault: %w", err)
	}

	fmt.Printf("Postgres password stored in Key Vault '%s' as secret '%s'.\n", keyVaultName, secretName)

	return nil
}


// storeSecretInKeyVault stores a secret in Azure Key Vault
func storeSecretInKeyVault(ctx context.Context, subscriptionID, keyVaultName, secretName, secretValue string) error {
	vaultUrl := fmt.Sprintf("https://%s.vault.azure.net/", keyVaultName)
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get Azure credential: %w", err)
	}
	client, err := azsecrets.NewClient(vaultUrl, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	_, err = client.SetSecret(ctx, secretName, azsecrets.SetSecretParameters{Value: to.Ptr(secretValue)}, nil)
	if err != nil {
		return fmt.Errorf("failed to set secret in Key Vault: %w", err)
	}
	return nil
}

// resetPostgresAdminPassword resets the admin password for the given Postgres server
func resetPostgresAdminPassword(ctx context.Context, subscriptionID, resourceGroup, serverName, adminUsername, newPassword string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get Azure credential: %w", err)
	}
	client, err := armpostgresqlflexibleservers.NewServersClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Postgres servers client: %w", err)
	}

	// Update the admin password
	_, err = client.BeginUpdate(ctx, resourceGroup, serverName, armpostgresqlflexibleservers.ServerForUpdate{
		Properties: &armpostgresqlflexibleservers.ServerPropertiesForUpdate{
			AdministratorLoginPassword: to.Ptr(newPassword),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to update Postgres admin password: %w", err)
	}
	return nil
}
