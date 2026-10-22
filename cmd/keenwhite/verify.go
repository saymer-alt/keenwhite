package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/saymer-alt/keenwhite/internal/artifact"
)

// runVerifyWDTT is strictly read-only: it does not execute the supplied ELF,
// start network transports, or access router configuration.
func runVerifyWDTT(args []string) int {
	flags := flag.NewFlagSet("verify-wdtt", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	file := flags.String("file", "", "local WDTT client ELF path (never executed)")
	sha := flags.String("sha256", "", "trusted expected SHA256 from a separate provenance source")
	arch := flags.String("arch", "", "expected architecture: arm64 or mipsle")
	jsonMode := flags.Bool("json", false, "machine-readable verification result")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if len(flags.Args()) != 0 || *file == "" || *sha == "" || *arch == "" {
		fmt.Fprintln(os.Stderr, "usage: keenwhite verify-wdtt -file PATH -sha256 64-HEX -arch arm64|mipsle [-json]")
		return 2
	}
	report, err := artifact.VerifyWDTT(*file, *sha, *arch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: WDTT offline artifact verification:", err)
		return 1
	}
	if *jsonMode {
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return 1
		}
		return 0
	}
	fmt.Printf("PASS: WDTT hash + %s ELF architecture match; TCP/UDP E2E NOT_TESTED\n", report.Architecture)
	return 0
}
