package main

import (
	"flag"
	"fmt"
	"os"

	modelcheck "vortex/tools/phase1"
)

func main() {
	modelPath := flag.String("model", "model/lease-safety.go", "model source path")
	resultDir := flag.String("results", "artifacts/modelcheck", "result directory")
	flag.Parse()
	report, err := modelcheck.Run(*modelPath, *resultDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if report.Status != "passed" {
		os.Exit(1)
	}
}
