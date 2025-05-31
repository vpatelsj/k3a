package main

import (
	"github.com/spf13/cobra"
)

var nsgCmd = &cobra.Command{
	Use:   "nsg",
	Short: "Manage Azure Network Security Groups",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(nsgCmd)
}
