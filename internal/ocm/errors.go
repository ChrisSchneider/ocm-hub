package ocm

import (
	"context"
	"errors"
	"fmt"

	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Errors returned by Service. Transports map them to their own status codes.
var (
	ErrComponentUnknown      = errors.New("component unknown")
	ErrVersionUnknown        = errors.New("version unknown")
	ErrResourceUnknown       = errors.New("resource unknown")
	ErrResourceAmbiguous     = errors.New("resource ambiguous")
	ErrRepositoryInvalid     = errors.New("repository invalid")
	ErrRepositoryNotAllowed  = errors.New("repository not allowed")
	ErrAccessTypeUnsupported = errors.New("access type unsupported")
	ErrUpstream              = errors.New("upstream error")
	ErrUpstreamTimeout       = errors.New("upstream timeout")
)

// AmbiguousResourceError lists the identities that matched a resource request.
type AmbiguousResourceError struct {
	Candidates []ocmruntime.Identity
}

func (e *AmbiguousResourceError) Error() string {
	return fmt.Sprintf("%d resources match: %v", len(e.Candidates), e.Candidates)
}

// Is makes errors.Is(err, ErrResourceAmbiguous) true.
func (e *AmbiguousResourceError) Is(target error) bool { return target == ErrResourceAmbiguous }

// upstreamError classifies a binding error that is not a "not found".
func upstreamError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrUpstreamTimeout, err)
	}
	return fmt.Errorf("%w: %v", ErrUpstream, err)
}
