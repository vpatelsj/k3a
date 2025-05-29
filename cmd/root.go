package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var subscriptionID string

var rootCmd = &cobra.Command{
	Use:               "k3a",
	Short:             "k3s deployment and management tool for Azure",
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	rootCmd.PersistentFlags().StringVar(&subscriptionID, "subscription", "", "Azure subscription ID (or set AZURE_SUBSCRIPTION_ID)")
	_ = rootCmd.MarkPersistentFlagRequired("subscription")
}
