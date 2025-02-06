package procinfo

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type LoadAvg struct {
	Last1Min  float64
	Last5Min  float64
	Last15Min float64
}

func GetLoadAvg() (*LoadAvg, error) {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		panic(err)
	}

	loadavg := LoadAvg{}

	output := strings.TrimSpace(string(out))
	fields := strings.Fields(output)
	loadavg.Last1Min, _ = strconv.ParseFloat(fields[len(fields)-3], 64)
	loadavg.Last5Min, _ = strconv.ParseFloat(fields[len(fields)-2], 64)
	loadavg.Last15Min, _ = strconv.ParseFloat(fields[len(fields)-1], 64)

	fmt.Printf("Load Averages: 1min: %.2f, 5min: %.2f, 15min: %.2f\n", loadavg.Last1Min, loadavg.Last5Min, loadavg.Last15Min)
	return &loadavg, nil
}
