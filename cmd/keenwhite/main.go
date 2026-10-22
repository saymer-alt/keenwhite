package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/saymer-alt/keenwhite/internal/catalog"
)

// Version is set at build time. The development version must not imply production readiness.
var Version = "0.2.1-dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	if args[0] == "version" {
		fmt.Printf("KeenWhite %s (offline prototype; no network or lifecycle operations)\n", Version)
		return 0
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage()
		return 0
	}
	if args[0] == "verify-wdtt" {
		return runVerifyWDTT(args[1:])
	}
	cmd := args[0]
	if cmd != "validate" && cmd != "providers" && cmd != "doctor" {
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		usage()
		return 2
	}
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	path := flags.String("catalog", "examples/providers.json", "path to non-secret provider catalog")
	jsonMode := flags.Bool("json", false, "print machine-readable JSON (doctor only)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if len(flags.Args()) > 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		return 2
	}
	c, err := catalog.Load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	failures := catalog.Validate(c)
	if len(failures) > 0 {
		for _, e := range failures {
			fmt.Fprintln(os.Stderr, "FAIL:", e)
		}
		return 1
	}
	switch cmd {
	case "validate":
		fmt.Printf("PASS: %d provider manifest(s); catalog syntax and static conflicts only\n", len(c.Providers))
	case "providers":
		for _, p := range c.Providers {
			fmt.Printf("%s\t%s\t%s\t%s\tTCP:%s UDP:%s\n", p.ID, p.Name, p.Lifecycle, p.Bind, p.TCP, p.UDP)
		}
	case "doctor":
		type status struct {
			Provider      string `json:"provider"`
			Configuration string `json:"configuration"`
			Process       string `json:"process"`
			Listener      string `json:"listener"`
			TCPE2E        string `json:"tcp_e2e"`
			UDPE2E        string `json:"udp_e2e"`
			Reason        string `json:"reason"`
		}
		results := make([]status, 0, len(c.Providers))
		for _, p := range c.Providers {
			results = append(results, status{p.ID, "VALID", "NOT_TESTED", "NOT_TESTED", "NOT_TESTED", "NOT_TESTED", "offline-only: no PID, socket, server, route or secret inspected"})
		}
		if *jsonMode {
			payload, _ := json.MarshalIndent(struct {
				Tool    string   `json:"tool"`
				Mode    string   `json:"mode"`
				Results []status `json:"results"`
			}{"keenwhite", "offline", results}, "", "  ")
			fmt.Println(string(payload))
		} else {
			fmt.Printf("KeenWhite doctor (offline; %d providers): catalog VALID\n", len(results))
			for _, r := range results {
				fmt.Printf(" - %-12s config=%s process=%s listener=%s TCP_E2E=%s UDP_E2E=%s\n", r.Provider, r.Configuration, r.Process, r.Listener, r.TCPE2E, r.UDPE2E)
			}
			fmt.Println("No runtime readiness asserted; use hardware acceptance before deploying any provider.")
		}
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, `KeenWhite — independent, offline-first Keenetic transport manager (prototype)
Usage:
  keenwhite version
  keenwhite validate  [-catalog path]
  keenwhite providers [-catalog path]
  keenwhite doctor    [-catalog path] [-json]
  keenwhite verify-wdtt -file PATH -sha256 SHA256 -arch arm64|mipsle [-json]

No command starts a tunnel, edits router settings, or accesses the network.`)
}
