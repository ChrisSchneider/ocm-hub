// Command fipscheck is step 1 of the FIPS scanner (prototype): it reads the
// SBOMs of a component version's images (embedded or in the registry, via the
// hub's attestation discovery), writes SARIF findings for images that are
// definitely not FIPS 140 compliant and, with --attest, attaches the SARIF as
// an in-toto attestation (OCI referrer) to the component version.
//
//	fipscheck --config .ocmconfig [--out fips.sarif] [--attest] <repository>//<component>:<version>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
	"chrisschneider.dev/delivery-bundler/internal/compliance/fips"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fipscheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fipscheck", flag.ContinueOnError)
	configPath := fs.String("config", "", ".ocmconfig with registry credentials (required)")
	out := fs.String("out", "fips.sarif", "SARIF output file; - for stdout")
	attest := fs.Bool("attest", false, "attach the SARIF to the component version as an in-toto attestation (needs push rights)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || fs.NArg() != 1 {
		return errors.New("usage: fipscheck --config <.ocmconfig> [--out fips.sarif] [--attest] <repository>//<component>:<version>")
	}
	ref, err := compliance.ParseRef(fs.Arg(0))
	if err != nil {
		return err
	}
	c, err := compliance.Load(ctx, *configPath, ref)
	if err != nil {
		return err
	}
	defer c.Close()

	log := fips.Scan(c)
	b, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	if *out == "-" {
		_, err = os.Stdout.Write(append(b, '\n'))
	} else {
		err = os.WriteFile(*out, append(b, '\n'), 0o644)
	}
	if err != nil {
		return err
	}
	for _, img := range c.Images {
		fmt.Fprintf(os.Stderr, "%s: %d SBOM(s)\n", img.Name, len(c.SBOMs[img.Name]))
	}
	for _, r := range log.Runs[0].Results {
		fmt.Fprintf(os.Stderr, "  %-7s %s %s\n", r.Level, r.RuleID, r.Message.Text)
	}
	if *attest {
		at, err := c.Attest(ctx, fips.PredicateType, b)
		if err != nil {
			return fmt.Errorf("attest: %w", err)
		}
		fmt.Fprintln(os.Stderr, "attested:", at)
	}
	return nil
}
