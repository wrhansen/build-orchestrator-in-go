/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var stopLog *log.Logger

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running task.",
	Long: `cube stop command.

The stop command stops a running task.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		manager, _ := cmd.Flags().GetString("manager")
		url := fmt.Sprintf("http://%s/tasks/%s", manager, args[0])
		client := &http.Client{}
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			stopLog.Printf("Error creating request: %v: %v", url, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			stopLog.Printf("Error connecting to %v: %v", url, err)
		}

		if resp.StatusCode != http.StatusNoContent {
			stopLog.Printf("Error sending request: %v", err)
			return
		}

		stopLog.Printf("Task %v has been stopped.", args[0])
	},
}

func init() {
	stopLog = log.New(os.Stdout, "[stop] ", log.Ldate|log.Ltime)

	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().StringP("manager", "m", "localhost:5555", "Manager to talk to.")

}
