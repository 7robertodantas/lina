package internal

import (
	"sync"

	"github.com/redis/go-redis/v9"
)

// ClampConsumeParallelism bounds max concurrent handlers per stream batch (1–64).
func ClampConsumeParallelism(p int) int {
	if p < 1 {
		return 1
	}
	if p > 64 {
		return 64
	}
	return p
}

// RunStreamMessagesParallel runs runOne for each message with at most p concurrent goroutines.
func RunStreamMessagesParallel(p int, msgs []redis.XMessage, runOne func(redis.XMessage)) {
	p = ClampConsumeParallelism(p)
	if len(msgs) == 0 {
		return
	}
	if p == 1 || len(msgs) == 1 {
		for _, msg := range msgs {
			runOne(msg)
		}
		return
	}
	sem := make(chan struct{}, p)
	var wg sync.WaitGroup
	for _, msg := range msgs {
		msg := msg
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runOne(msg)
		}()
	}
	wg.Wait()
}
