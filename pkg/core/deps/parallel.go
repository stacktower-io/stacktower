package deps

import (
	"context"
	"sync"
)

type indexedResult[T any] struct {
	index int
	value T
}

// ParallelMapOrdered applies fn to items with bounded concurrency and returns
// results in the same order as the input slice.
//
// Context cancellation is respected: if the context is cancelled before or
// during execution, the function returns nil. Workers check context before
// processing each item and stop early on cancellation.
func ParallelMapOrdered[I any, O any](
	ctx context.Context,
	workers int,
	items []I,
	fn func(context.Context, I) O,
) []O {
	if len(items) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}
	workers = min(workers, len(items))

	// Bound channel capacity to worker count to avoid O(n) memory allocation
	// for large item slices. Workers pull from jobs as they complete work.
	jobs := make(chan int, workers)
	results := make(chan indexedResult[O], workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				results <- indexedResult[O]{
					index: index,
					value: fn(ctx, items[index]),
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- index:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	ordered := make([]O, len(items))
	received := 0
	for result := range results {
		ordered[result.index] = result.value
		received++
	}

	if ctx.Err() != nil {
		return nil
	}
	return ordered
}
