package ocm

import "sync"

// treeConcurrency bounds how many references of one component are fetched at once.
const treeConcurrency = 8

// forEachLimit calls fn(0) … fn(n-1) with at most limit calls running at once
// and returns when all have returned.
func forEachLimit(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range n {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(i)
		})
	}
	wg.Wait()
}
