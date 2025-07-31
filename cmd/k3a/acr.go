package main

import (
	"fmt"

	"github.com/jwilder/k3a/acr"
	"github.com/jwilder/k3a/kubeconfig"
	"github.com/jwilder/k3a/pkg/spinner"
	"github.com/spf13/cobra"
)

var acrCmd = &cobra.Command{
	Use:   "acr",
	Short: "Azure Container Registry management commands",
}

var createACRCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an ACR for container image storage",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		acrName, _ := cmd.Flags().GetString("name")
		if acrName == "" {
			return fmt.Errorf("--name flag is required")
		}
		region, _ := cmd.Flags().GetString("region")
		if region == "" {
			return fmt.Errorf("--region flag is required")
		}
		sku, _ := cmd.Flags().GetString("sku")
		createSecret, _ := cmd.Flags().GetBool("create-secret")
		secretName, _ := cmd.Flags().GetString("secret-name")
		namespace, _ := cmd.Flags().GetString("namespace")

		done := spinner.Spinner(fmt.Sprintf("Starting ACR '%s' creation in region '%s'...", acrName, region))

		if err := acr.Create(acr.CreateArgs{
			SubscriptionID: subscriptionID,
			Cluster:        clusterName,
			ACRName:        acrName,
			Location:       region,
			SKU:            sku,
		}); err != nil {
			done()
			return fmt.Errorf("failed to create ACR: %w", err)
		}

		done()
		fmt.Printf("ACR '%s' created successfully in region '%s'\n", acrName, region)

		if createSecret {
			fmt.Println("Downloading kubeconfig and creating imagePullSecret...")

			// Download kubeconfig
			kubeDone := spinner.Spinner("Downloading kubeconfig from Key Vault...")
			if err := kubeconfig.Download(kubeconfig.DownloadArgs{
				Cluster: clusterName,
			}); err != nil {
				kubeDone()
				return fmt.Errorf("failed to download kubeconfig: %w", err)
			}
			kubeDone()
			fmt.Println("Kubeconfig downloaded successfully")

			// Create imagePullSecret
			secretDone := spinner.Spinner(fmt.Sprintf("Creating imagePullSecret '%s'...", secretName))
			if err := acr.CreateImagePullSecret(acr.ImagePullSecretArgs{
				SubscriptionID: subscriptionID,
				Cluster:        clusterName,
				ACRName:        acrName,
				SecretName:     secretName,
				Namespace:      namespace,
			}); err != nil {
				secretDone()
				return fmt.Errorf("failed to create imagePullSecret: %w", err)
			}
			secretDone()
			fmt.Printf("ImagePullSecret '%s' created successfully in namespace '%s'\n", secretName, namespace)

			fmt.Printf("\nYou can now use the ACR in your pods by referencing the imagePullSecret:\n")
			fmt.Printf("  imagePullSecrets:\n")
			fmt.Printf("  - name: %s\n", secretName)
		}

		return nil
	},
}

var listACRCmd = &cobra.Command{
	Use:   "list",
	Short: "List ACR instances for a cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}

		return acr.List(acr.ListArgs{
			SubscriptionID: subscriptionID,
			Cluster:        clusterName,
		})
	},
}

var createSecretCmd = &cobra.Command{
	Use:   "create-secret",
	Short: "Create imagePullSecret for an existing ACR",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		acrName, _ := cmd.Flags().GetString("name")
		if acrName == "" {
			return fmt.Errorf("--name flag is required")
		}
		secretName, _ := cmd.Flags().GetString("secret-name")
		namespace, _ := cmd.Flags().GetString("namespace")

		fmt.Println("Downloading kubeconfig and creating imagePullSecret...")

		// Download kubeconfig
		kubeDone := spinner.Spinner("Downloading kubeconfig from Key Vault...")
		if err := kubeconfig.Download(kubeconfig.DownloadArgs{
			Cluster: clusterName,
		}); err != nil {
			kubeDone()
			return fmt.Errorf("failed to download kubeconfig: %w", err)
		}
		kubeDone()
		fmt.Println("Kubeconfig downloaded successfully")

		// Create imagePullSecret
		secretDone := spinner.Spinner(fmt.Sprintf("Creating imagePullSecret '%s'...", secretName))
		if err := acr.CreateImagePullSecret(acr.ImagePullSecretArgs{
			SubscriptionID: subscriptionID,
			Cluster:        clusterName,
			ACRName:        acrName,
			SecretName:     secretName,
			Namespace:      namespace,
		}); err != nil {
			secretDone()
			return fmt.Errorf("failed to create imagePullSecret: %w", err)
		}
		secretDone()
		fmt.Printf("ImagePullSecret '%s' created successfully in namespace '%s'\n", secretName, namespace)

		fmt.Printf("\nYou can now use the ACR in your pods by referencing the imagePullSecret:\n")
		fmt.Printf("  imagePullSecrets:\n")
		fmt.Printf("  - name: %s\n", secretName)

		return nil
	},
}

var deleteACRCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an ACR instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		subscriptionID, _ := cmd.Root().Flags().GetString("subscription")
		if subscriptionID == "" {
			return fmt.Errorf("--subscription flag is required (or set K3A_SUBSCRIPTION)")
		}
		clusterName, _ := cmd.Flags().GetString("cluster")
		if clusterName == "" {
			return fmt.Errorf("--cluster flag is required (or set K3A_CLUSTER)")
		}
		acrName, _ := cmd.Flags().GetString("name")
		if acrName == "" {
			return fmt.Errorf("--name flag is required")
		}

		done := spinner.Spinner(fmt.Sprintf("Deleting ACR '%s'...", acrName))
		defer done()

		if err := acr.Delete(acr.DeleteArgs{
			SubscriptionID: subscriptionID,
			Cluster:        clusterName,
			ACRName:        acrName,
		}); err != nil {
			return fmt.Errorf("failed to delete ACR: %w", err)
		}
		fmt.Printf("ACR '%s' deleted successfully\n", acrName)
		return nil
	},
}

func init() {
	// ACR create flags
	createACRCmd.Flags().String("cluster", "", "Cluster name (or set K3A_CLUSTER) (required)")
	createACRCmd.Flags().String("name", "", "ACR registry name (required)")
	createACRCmd.Flags().String("region", "", "Azure region for the ACR (e.g., canadacentral) (required)")
	createACRCmd.Flags().String("sku", "Basic", "ACR SKU (Basic, Standard, Premium)")
	createACRCmd.Flags().Bool("create-secret", false, "Download kubeconfig and create imagePullSecret")
	createACRCmd.Flags().String("secret-name", "acr-secret", "Name for the imagePullSecret")
	createACRCmd.Flags().String("namespace", "default", "Namespace for the imagePullSecret")
	_ = createACRCmd.MarkFlagRequired("cluster")
	_ = createACRCmd.MarkFlagRequired("name")
	_ = createACRCmd.MarkFlagRequired("region")

	// ACR list flags
	listACRCmd.Flags().String("cluster", "", "Cluster name (or set K3A_CLUSTER) (required)")
	_ = listACRCmd.MarkFlagRequired("cluster")

	// ACR delete flags
	deleteACRCmd.Flags().String("cluster", "", "Cluster name (or set K3A_CLUSTER) (required)")
	deleteACRCmd.Flags().String("name", "", "ACR registry name (required)")
	_ = deleteACRCmd.MarkFlagRequired("cluster")
	_ = deleteACRCmd.MarkFlagRequired("name")

	// ACR create-secret flags
	createSecretCmd.Flags().String("cluster", "", "Cluster name (or set K3A_CLUSTER) (required)")
	createSecretCmd.Flags().String("name", "", "ACR registry name (required)")
	createSecretCmd.Flags().String("secret-name", "acr-secret", "Name for the imagePullSecret")
	createSecretCmd.Flags().String("namespace", "default", "Namespace for the imagePullSecret")
	_ = createSecretCmd.MarkFlagRequired("cluster")
	_ = createSecretCmd.MarkFlagRequired("name")

	// Add all subcommands to acrCmd
	acrCmd.AddCommand(createACRCmd, listACRCmd, deleteACRCmd, createSecretCmd)

	// Register acrCmd with rootCmd
	rootCmd.AddCommand(acrCmd)
}
