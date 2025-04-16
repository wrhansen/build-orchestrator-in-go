/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var runLog *log.Logger

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a new task.",
	Long: `cube run command.

The run command starts a new task.`,
	Run: func(cmd *cobra.Command, args []string) {
		manager, _ := cmd.Flags().GetString("manager")
		filename, _ := cmd.Flags().GetString("filename")
		fullFilePath, err := filepath.Abs(filename)
		if err != nil {
			runLog.Fatal(err)
		}
		if !fileExists(fullFilePath) {
			runLog.Fatalf("File %s does not exist", filename)
		}

		runLog.Printf("Using manager: %v\n", manager)
		runLog.Printf("Using file: %v\n", fullFilePath)
		data, err := os.ReadFile(filename)
		if err != nil {
			runLog.Fatalf("Unable to read file: %v", filename)
		}
		runLog.Printf("Data: %v\n", string(data))

		url := fmt.Sprintf("http://%s/tasks", manager)
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
		if err != nil {
			runLog.Panic(err)
		}

		if resp.StatusCode != http.StatusCreated {
			runLog.Printf("Error sending request: %v", resp.StatusCode)
		}

		defer resp.Body.Close()
		runLog.Println("Successfully sent task request to manager")
	},
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !errors.Is(err, fs.ErrNotExist)
}

func init() {
	runLog = log.New(os.Stdout, "[run] ", log.Ldate|log.Ltime)

	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringP("manager", "m", "localhost:5555", "Manager to talk to")
	runCmd.Flags().StringP("filename", "f", "task.json", "Task specification file")
}
