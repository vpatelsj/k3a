package acr

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
)

type ImagePullSecretArgs struct {
	SubscriptionID string
	Cluster        string
	ACRName        string
	SecretName     string
	Namespace      string
}

// CreateImagePullSecret creates a Kubernetes imagePullSecret for the ACR
func CreateImagePullSecret(args ImagePullSecretArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Get ACR login server
	registriesClient, err := armcontainerregistry.NewRegistriesClient(args.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create registries client: %w", err)
	}

	registry, err := registriesClient.Get(ctx, args.Cluster, args.ACRName, nil)
	if err != nil {
		return fmt.Errorf("failed to get ACR %s: %w", args.ACRName, err)
	}

	if registry.Properties == nil || registry.Properties.LoginServer == nil {
		return fmt.Errorf("ACR %s does not have a login server", args.ACRName)
	}

	loginServer := *registry.Properties.LoginServer

	// Get ACR access token using Azure CLI
	token, err := getACRToken(args.ACRName)
	if err != nil {
		return fmt.Errorf("failed to get ACR token: %w", err)
	}

	// Create the imagePullSecret using kubectl
	if err := createKubernetesSecret(args.SecretName, args.Namespace, loginServer, token); err != nil {
		return fmt.Errorf("failed to create Kubernetes secret: %w", err)
	}

	return nil
}

// getACRToken gets an ACR access token using Azure CLI
func getACRToken(acrName string) (string, error) {
	cmd := exec.Command("az", "acr", "login", "--name", acrName, "--expose-token", "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run az acr login: %w", err)
	}

	var result struct {
		AccessToken string `json:"accessToken"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("failed to parse az acr login output: %w", err)
	}

	return result.AccessToken, nil
}

// createKubernetesSecret creates a Docker registry secret in Kubernetes
func createKubernetesSecret(secretName, namespace, loginServer, token string) error {
	// Use kubectl to create the secret
	args := []string{
		"create", "secret", "docker-registry", secretName,
		"--docker-server=" + loginServer,
		"--docker-username=00000000-0000-0000-0000-000000000000",
		"--docker-password=" + token,
	}

	if namespace != "" && namespace != "default" {
		args = append(args, "--namespace="+namespace)
	}

	// Check if secret already exists and delete it first
	checkCmd := exec.Command("kubectl", "get", "secret", secretName)
	if namespace != "" && namespace != "default" {
		checkCmd.Args = append(checkCmd.Args, "--namespace="+namespace)
	}

	if checkCmd.Run() == nil {
		// Secret exists, delete it first
		deleteArgs := []string{"delete", "secret", secretName}
		if namespace != "" && namespace != "default" {
			deleteArgs = append(deleteArgs, "--namespace="+namespace)
		}
		deleteCmd := exec.Command("kubectl", deleteArgs...)
		deleteCmd.Run() // Ignore error if secret doesn't exist
	}

	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes secret: %w, output: %s", err, string(output))
	}

	return nil
}
