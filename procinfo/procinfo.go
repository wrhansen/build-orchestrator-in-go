package procinfo

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func GetLoadAvg() {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		panic(err)
	}

	output := strings.TrimSpace(string(out))
	fields := strings.Fields(output)
	loadAvg1, _ := strconv.ParseFloat(fields[len(fields)-3], 64)
	loadAvg5, _ := strconv.ParseFloat(fields[len(fields)-2], 64)
	loadAvg15, _ := strconv.ParseFloat(fields[len(fields)-1], 64)

	fmt.Printf("Load Averages: 1min: %.2f, 5min: %.2f, 15min: %.2f\n", loadAvg1, loadAvg5, loadAvg15)
}
