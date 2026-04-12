package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/robertodantas/lina/internal"
)

var warnLightningJanitorUnsupported sync.Once

// RunLightningStreamJanitor periodically trims event.lightning using XTRIM … ACKED so entries are only
// removed after every consumer group has acknowledged them (ledger + device).
//
// Redis semantics (important):
//   - XTRIM MAXLEN N only evicts when stream length is *greater than* N. With a huge N (e.g. 100k),
//     small streams are never trimmed even if every entry is ACKed — that is expected.
//   - After the configured MAXLEN pass, we run repeated XTRIM MAXLEN 1 … ACKED to shrink the backlog
//     toward a single entry. One fully-acknowledged entry can still remain when length is already 1
//     (MAXLEN 1 is satisfied without deleting).
//
// Disable via LEDGER_LIGHTNING_JANITOR_ENABLED=false. Requires Redis with ACKED trim (8.2+).
func (ewsi *EastWestStreamInterface) RunLightningStreamJanitor(ctx context.Context) {
	cfg := ewsi.cfg
	if !cfg.LightningJanitorEnabled {
		logger.Info(ctx, "Lightning stream janitor disabled (LEDGER_LIGHTNING_JANITOR_ENABLED=false)")
		return
	}
	interval := cfg.LightningJanitorInterval
	if interval < time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Infof(ctx, "Starting lightning stream janitor (interval=%s maxlen=%d approx=%v mode=ACKED)",
		interval, cfg.LightningJanitorMaxLen, cfg.LightningJanitorApprox)

	ewsi.runLightningStreamJanitorOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Stopping lightning stream janitor")
			return
		case <-ticker.C:
			ewsi.runLightningStreamJanitorOnce(ctx)
		}
	}
}

const lightningJanitorMaxChipIterations = 100000

func (ewsi *EastWestStreamInterface) runLightningStreamJanitorOnce(ctx context.Context) {
	cfg := ewsi.cfg
	if cfg.LightningJanitorMaxLen <= 0 {
		return
	}
	c := ewsi.Client()
	stream := internal.StreamLightning
	mode := "ACKED"

	var total int64

	// Pass 1: cap stream length when it exceeds LightningJanitorMaxLen.
	var n int64
	var err error
	if cfg.LightningJanitorApprox {
		limit := cfg.LightningJanitorApproxLimit
		if limit <= 0 {
			limit = 10000
		}
		n, err = c.XTrimMaxLenApproxMode(ctx, stream, cfg.LightningJanitorMaxLen, limit, mode).Result()
	} else {
		n, err = c.XTrimMaxLenMode(ctx, stream, cfg.LightningJanitorMaxLen, mode).Result()
	}
	if err != nil {
		if isUnknownOrUnsupportedRedisCommand(err) {
			warnLightningJanitorUnsupported.Do(func() {
				logger.Warnf(ctx, "Lightning stream janitor: XTRIM … ACKED not supported on this Redis (disable with LEDGER_LIGHTNING_JANITOR_ENABLED=false or upgrade Redis): %v", err)
			})
			return
		}
		logger.WithStream(stream, "trim").
			Warnf(ctx, "XTRIM MAXLEN ACKED failed: %v", err)
		return
	}
	total += n

	// Pass 2: MAXLEN 1 repeatedly — removes one eligible (all-groups-acknowledged) entry per successful
	// trim until the stream has at most one entry or Redis returns 0 (nothing left to trim under ACKED rules).
	for i := 0; i < lightningJanitorMaxChipIterations; i++ {
		n2, err2 := c.XTrimMaxLenMode(ctx, stream, 1, mode).Result()
		if err2 != nil {
			logger.WithStream(stream, "trim").
				Warnf(ctx, "XTRIM MAXLEN 1 ACKED (chip) failed: %v", err2)
			return
		}
		if n2 == 0 {
			break
		}
		total += n2
	}

	if total > 0 {
		logger.WithStream(stream, "trim").
			Infof(ctx, "Trimmed %d entries total (maxlen_pass=%v maxlen=%d + chip MAXLEN 1 ACKED)", total, cfg.LightningJanitorApprox, cfg.LightningJanitorMaxLen)
	}
}

func isUnknownOrUnsupportedRedisCommand(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unknown command") ||
		strings.Contains(s, "syntax error") ||
		strings.Contains(s, "wrong number") ||
		strings.Contains(s, "invalid argument")
}
