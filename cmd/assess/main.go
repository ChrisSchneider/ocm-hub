// Command assess writes a BSI-flavoured OSCAL assessment result for a
// component version (prototype): every image has an SBOM and the FIPS
// findings of the newest cmd/fipscheck attestation on the component. With
// --attest the result is attached to the component version as well.
//
//	assess --config .ocmconfig [--out assessment-results.json] [--attest] <repository>//<component>:<version>
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
	"time"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
	"chrisschneider.dev/delivery-bundler/internal/compliance/fips"
	"chrisschneider.dev/delivery-bundler/internal/compliance/oscal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "assess:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("assess", flag.ContinueOnError)
	configPath := fs.String("config", "", ".ocmconfig with registry credentials (required)")
	out := fs.String("out", "assessment-results.json", "OSCAL output file; - for stdout")
	attest := fs.Bool("attest", false, "attach the result to the component version as an in-toto attestation (needs push rights)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || fs.NArg() != 1 {
		return errors.New("usage: assess --config <.ocmconfig> [--out assessment-results.json] [--attest] <repository>//<component>:<version>")
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

	var fipsLog *compliance.SARIF
	evidence := ""
	b, at, ok, err := c.LatestAttestation(ctx, fips.PredicateType)
	switch {
	case err != nil:
		return fmt.Errorf("FIPS attestation: %w", err)
	case !ok:
		fmt.Fprintln(os.Stderr, "no FIPS attestation on the component (run fipscheck --attest): FIPS requirement skipped")
	default:
		l, err := compliance.ParseSARIF(b)
		if err != nil {
			return fmt.Errorf("FIPS attestation %s: %w", at, err)
		}
		fipsLog, evidence = &l, "oci://"+at
		fmt.Fprintln(os.Stderr, "FIPS attestation:", at)
	}

	checks := oscal.Checks(c, fipsLog, evidence)
	b, err = json.MarshalIndent(oscal.Build(ref, checks, time.Now()), "", "  ")
	if err != nil {
		return err
	}
	for _, ch := range checks {
		fmt.Fprintf(os.Stderr, "%-8s %-8s pass=%-5v %s\n", ch.Requirement.ID, ch.Image, ch.Pass, ch.Detail)
	}
	if *out == "-" {
		_, err = os.Stdout.Write(append(b, '\n'))
	} else {
		err = os.WriteFile(*out, append(b, '\n'), 0o644)
	}
	if err != nil || !*attest {
		return err
	}
	at, err = c.Attest(ctx, oscal.PredicateType, b)
	if err != nil {
		return fmt.Errorf("attest: %w", err)
	}
	fmt.Fprintln(os.Stderr, "attested:", at)
	return nil
}
