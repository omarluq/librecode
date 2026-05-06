package main

import "github.com/spf13/cobra"

// NewRootCmdForTest exposes the root command constructor for external tests.
func NewRootCmdForTest() *cobra.Command { return newRootCmd() }
