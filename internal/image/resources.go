package image

import (
	"context"
	"fmt"
	"maps"
	"sync"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
)

// resourceConcurrency bounds parallel image reads of one descriptor.
const resourceConcurrency = 4

// ResourcePlatforms reads the platforms of every ociImage or ociArtifact
// resource of d that names an OCI subject, a few at a time. A failed read
// leaves out that resource's platforms and adds its error; the platforms may
// repeat across resources. The empty platform of a single manifest without
// os/arch is left out.
func ResourcePlatforms(ctx context.Context, open attestation.RepositoryOpener, d *descriptorruntime.Descriptor, location string) ([]string, []error) {
	var (
		mu        sync.Mutex
		platforms []string
		errs      []error
		wg        sync.WaitGroup
	)
	sem := make(chan struct{}, resourceConcurrency)
	for _, r := range d.Component.Resources {
		if r.Type != "ociImage" && r.Type != "ociArtifact" {
			continue
		}
		id := ocmruntime.Identity{"name": r.Name}
		maps.Copy(id, r.ExtraIdentity)
		s, err := attestation.ResourceSubject(d, location, id)
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("resource %s: %w", r.Name, err))
			mu.Unlock()
			continue
		}
		if s == (attestation.Subject{}) {
			continue
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			idx, err := Platforms(ctx, open, s)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("resource %s: %w", r.Name, err))
				return
			}
			for _, p := range idx.Platforms {
				if p.Platform != "" {
					platforms = append(platforms, p.Platform)
				}
			}
		})
	}
	wg.Wait()
	return platforms, errs
}
