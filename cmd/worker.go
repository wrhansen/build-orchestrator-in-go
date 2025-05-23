package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/wrhansen/build-orchestrator-in-go/worker"
)

var workerLog *log.Logger

// workerCmd represents the worker command
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Worker command to operate a Cube worker node.",
	Long: `cube worker command.

The worker runs tasks and responds to the manager's requests about task state.`,
	Run: func(cmd *cobra.Command, args []string) {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		name, _ := cmd.Flags().GetString("name")
		dbType, _ := cmd.Flags().GetString("dbtype")

		workerLog.Println("Starting worker.")
		w := worker.New(name, dbType)
		api := worker.Api{Address: host, Port: port, Worker: w}
		go w.RunTasks()
		go w.CollectStats()
		go w.UpdateTasks()
		workerLog.Printf("Starting worker API on http://%s:%d", host, port)
		api.Start()
	},
}

func init() {
	workerLog = log.New(os.Stdout, "[worker] ", log.Ldate|log.Ltime)

	rootCmd.AddCommand(workerCmd)
	workerCmd.Flags().StringP("host", "H", "0.0.0.0", "Hostname or IP address")
	workerCmd.Flags().IntP("port", "p", 5556, "Port on which to listen")
	workerCmd.Flags().StringP("name", "n", fmt.Sprintf("worker-%s", uuid.New().String()), "Name of the worker")
	workerCmd.Flags().StringP("dbtype", "d", "memory", "Type of database to use (\"memory\" or \"persistent\")")
}
