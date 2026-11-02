// Package sched runs due check-ins in-process. No cron infrastructure needed:
// the binary ticks, finds active trials whose next_check has passed, and runs
// App.CheckIn on each. The demo "fast-forward" button hits the same code path
// via POST /api/trials/{id}/checkin.
package sched

import (
	"context"
	"log"
	"time"

	"github.com/manasnikam/skinverdict/internal/app"
)

func Run(ctx context.Context, a *app.App, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick(ctx, a)
		}
	}
}

func tick(ctx context.Context, a *app.App) {
	trials, err := a.Store.Trials()
	if err != nil {
		log.Printf("sched: %v", err)
		return
	}
	now := time.Now()
	for _, tr := range trials {
		if tr.Status != "active" || tr.NextCheck.After(now) {
			continue
		}
		scans, _ := a.Store.Scans(tr.ID)
		if len(scans) < 2 {
			continue // nothing new to judge; wait for the shopper's next scan
		}
		if _, v, err := a.CheckIn(ctx, tr.ID); err != nil {
			log.Printf("sched: trial %s: %v", tr.ID, err)
		} else {
			log.Printf("sched: trial %s -> %s (Δ %.1f)", tr.ID, v.Status, v.Delta)
		}
	}
}
