/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package sharedcli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
)

const (
	usageFmt = "Usage:\n  %s\n"
)

// generatesAvailableSubCommands generates command's subcommand information which
// is usually part of a help message. E.g.:
//
// Available Commands:
//
//	controller-manager completion                      generate the autocompletion script for the specified shell
//	controller-manager help                            Help about any command
//	controller-manager version                         Print the version information.
func generatesAvailableSubCommands(cmd *cobra.Command) []string {
	if !cmd.HasAvailableSubCommands() {
		return nil
	}

	info := []string{"\nAvailable Commands:"}
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			info = append(info, fmt.Sprintf("  %s %-30s  %s", cmd.CommandPath(), sub.Name(), sub.Short))
		}
	}
	return info
}

// SetUsageAndHelpFunc set both usage and help function.
func SetUsageAndHelpFunc(cmd *cobra.Command, fss cliflag.NamedFlagSets, cols int) {
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		if _, err := fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine()); err != nil {
			klog.Warning("failed to print usage: ", err.Error())
		}
		if cmd.HasAvailableSubCommands() {
			if _, err := fmt.Fprintf(cmd.OutOrStderr(), "%s\n",
				strings.Join(generatesAvailableSubCommands(cmd), "\n")); err != nil {
				klog.Warning("failed to print usage subcommand: ", err.Error())
			}
		}
		cliflag.PrintSections(cmd.OutOrStderr(), fss, cols)
		return nil
	})

	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine()); err != nil {
			klog.Warning("failed to print help: ", err.Error())
		}
		if cmd.HasAvailableSubCommands() {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n",
				strings.Join(generatesAvailableSubCommands(cmd), "\n")); err != nil {
				klog.Warning("failed to print help subcommand: ", err.Error())
			}
		}
		cliflag.PrintSections(cmd.OutOrStdout(), fss, cols)
	})
}
