package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/saymer-alt/keenwhite/internal/catalog"
	"github.com/saymer-alt/keenwhite/internal/contract"
)

// Version is set at build time. The development version must not imply production readiness.
var Version = "0.2.2-dev"

// Swappable output sinks so tests can call run() and inspect the exact
// streams (issue #32). Warnings are part of a successful report and go to
// stdout; only usage, decode errors and ERROR findings go to stderr.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	if args[0] == "version" {
		fmt.Fprintf(stdout, "KeenWhite %s (offline prototype; no network or lifecycle operations)\n", Version)
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
		fmt.Fprintln(stderr, "unknown command:", cmd)
		usage()
		return 2
	}
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("catalog", "examples/providers.json", "path to non-secret provider catalog")
	jsonMode := flags.Bool("json", false, "print machine-readable JSON (doctor only)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if len(flags.Args()) > 0 {
		fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	c, err := catalog.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	findings, effective := catalog.ValidateDetailed(c, time.Now())
	var errs, warns []contract.Finding
	for _, f := range findings {
		if f.Severity == contract.SeverityError {
			errs = append(errs, f)
		} else {
			warns = append(warns, f)
		}
	}
	// ERROR blocks every command: an invalid catalog cannot back diagnostic
	// output. Warnings are advisory and never change the exit code (issue #32).
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(stderr, "FAIL: [%s] %s: %s\n", e.Code, e.Field, e.Message)
		}
		return 1
	}
	switch cmd {
	case "validate":
		return runValidate(c, warns)
	case "providers":
		return runProviders(c, effective, warns)
	case "doctor":
		return runDoctor(c, effective, warns, *jsonMode)
	}
	return 2
}

func runValidate(c catalog.Catalog, warns []contract.Finding) int {
	for _, w := range warns {
		fmt.Fprintf(stdout, "WARNING [%s] %s: %s\n", w.Code, w.Field, w.Message)
	}
	if len(warns) > 0 {
		fmt.Fprintf(stdout, "PASS with %d warning(s): %d provider manifest(s); unverifiable E2E claims are shown at their effective level (UNKNOWN), never dropped\n",
			len(warns), len(c.Providers))
	} else {
		fmt.Fprintf(stdout, "PASS: %d provider manifest(s); catalog syntax and static conflicts only\n", len(c.Providers))
	}
	return 0
}

func runProviders(c catalog.Catalog, eff []catalog.EffectiveEvidence, warns []contract.Finding) int {
	effByID := map[string]catalog.EffectiveEvidence{}
	for _, e := range eff {
		effByID[e.ProviderID] = e
	}
	downgrades := 0
	for _, p := range c.Providers {
		e := effByID[p.ID]
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\tTCP:%s UDP:%s\n", p.ID, p.Name, p.Lifecycle, p.Bind, e.TCP, e.UDP)
		if e.TCP != p.TCP {
			downgrades++
		}
		if e.UDP != p.UDP {
			downgrades++
		}
	}
	if downgrades > 0 {
		fmt.Fprintf(stdout, "note: %d evidence claim(s) shown at their effective level (downgraded to UNKNOWN; run 'validate' for details)\n", downgrades)
	}
	if len(warns) > 0 {
		fmt.Fprintf(stdout, "note: %d warning(s) present; run 'validate' for the full list\n", len(warns))
	}
	return 0
}

func runDoctor(c catalog.Catalog, eff []catalog.EffectiveEvidence, warns []contract.Finding, jsonMode bool) int {
	effByID := map[string]catalog.EffectiveEvidence{}
	for _, e := range eff {
		effByID[e.ProviderID] = e
	}
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
		e := effByID[p.ID]
		results = append(results, status{
			Provider: p.ID, Configuration: "VALID", Process: "NOT_TESTED", Listener: "NOT_TESTED",
			TCPE2E: e.TCP, UDPE2E: e.UDP,
			Reason: "offline-only: no PID, socket, server, route or secret inspected; evidence levels are the catalog's EFFECTIVE levels",
		})
	}
	// Text and JSON carry identical semantics (issue #32): same effective
	// levels, same warning list — only the rendering differs.
	if jsonMode {
		payload, _ := json.MarshalIndent(struct {
			Tool     string             `json:"tool"`
			Mode     string             `json:"mode"`
			Results  []status           `json:"results"`
			Warnings []contract.Finding `json:"warnings"`
		}{"keenwhite", "offline", results, warns}, "", "  ")
		fmt.Fprintln(stdout, string(payload))
	} else {
		fmt.Fprintf(stdout, "KeenWhite doctor (offline; %d providers): catalog VALID\n", len(results))
		for _, r := range results {
			fmt.Fprintf(stdout, " - %-12s config=%s process=%s listener=%s TCP_E2E=%s UDP_E2E=%s\n", r.Provider, r.Configuration, r.Process, r.Listener, r.TCPE2E, r.UDPE2E)
		}
		for _, w := range warns {
			fmt.Fprintf(stdout, " WARNING [%s] %s: %s\n", w.Code, w.Field, w.Message)
		}
		fmt.Fprintln(stdout, "No runtime readiness asserted; use hardware acceptance before deploying any provider.")
	}
	return 0
}

func usage() {
	fmt.Fprintln(stderr, `KeenWhite — independent, offline-first Keenetic transport manager (prototype)
Usage:
  keenwhite version
  keenwhite validate  [-catalog path]
  keenwhite providers [-catalog path]
  keenwhite doctor    [-catalog path] [-json]
  keenwhite verify-wdtt -file PATH -sha256 SHA256 -arch arm64|mipsle [-json]

Diagnostics: ERROR blocks (exit 1); WARNING is advisory (exit 0, printed);
unverifiable E2E claims are shown at their EFFECTIVE level (UNKNOWN).
No command starts a tunnel, edits router settings, or accesses the network.`)
}
