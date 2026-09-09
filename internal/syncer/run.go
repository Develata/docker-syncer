package syncer

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
	"time"
)

type Result struct {
	Source, Platform, Target, Status, Reason string
	Duration                                 time.Duration
}
type RunOptions struct {
	Force, DryRun bool
	Log           io.Writer
}

// Run is sequential and deterministic. A source is resolved once per entry,
// then independently copied to each destination. A failed target cannot hide
// another target's result. Cancellation stops starting further network work.
func Run(ctx context.Context, s Skopeo, plans []Plan, options RunOptions) ([]Result, error) {
	results := make([]Result, 0, len(plans))
	failed := 0
	emit := func(r Result) {
		results = append(results, r)
		if r.Status == "FAILED" {
			failed++
		}
		if options.Log != nil {
			fmt.Fprintf(options.Log, "  %s -> %s [%s] %.3fs %s\n", r.Platform, r.Target, r.Status, r.Duration.Seconds(), concise(r.Reason))
		}
	}
	for i, plan := range plans {
		e := plan.Entry
		if options.Log != nil {
			fmt.Fprintf(options.Log, "[%d/%d] %s\n", i+1, len(plans), e.Source)
		}
		if len(plan.Targets) == 0 {
			emit(Result{Source: e.Source, Platform: e.Platform.String(), Target: "-", Status: "SKIPPED", Reason: "mode none"})
			continue
		}
		if options.DryRun {
			for _, target := range plan.Targets {
				emit(Result{Source: e.Source, Platform: e.Platform.String(), Target: target, Status: "SKIPPED", Reason: "dry-run: planned copy; no registry requests"})
			}
			continue
		}
		start := time.Now()
		source, digest, resolveErr := s.Resolve(ctx, e)
		for _, target := range plan.Targets {
			r := Result{Source: e.Source, Platform: e.Platform.String(), Target: target, Status: "FAILED"}
			targetStart := time.Now()
			err := resolveErr
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				var before []byte
				if !options.Force {
					before, err = s.inspect(ctx, target, false)
					if missing(err) {
						err = nil
					}
				}
				if err == nil && before != nil && rawDigest(before) == digest {
					r.Status = "UNCHANGED"
				} else if err == nil {
					err = s.Copy(ctx, source, target, e.Platform.All)
					if err == nil {
						var after []byte
						after, err = s.inspect(ctx, target, false)
						if err == nil && rawDigest(after) != digest {
							err = errors.New("post-copy target digest mismatch (registry conversion or concurrent writer)")
						}
						if err == nil {
							r.Status = "SYNCED"
						}
					}
				}
			}
			if err != nil {
				r.Reason = concise(err.Error())
			}
			r.Duration = time.Since(targetStart)
			if resolveErr != nil {
				r.Duration = time.Since(start)
			}
			emit(r)
		}
	}
	if options.Log != nil {
		counts := map[string]int{}
		for _, r := range results {
			counts[r.Status]++
		}
		fmt.Fprintf(options.Log, "Summary: SYNCED=%d UNCHANGED=%d FAILED=%d SKIPPED=%d\n", counts["SYNCED"], counts["UNCHANGED"], counts["FAILED"], counts["SKIPPED"])
	}
	if failed > 0 {
		return results, fmt.Errorf("%d synchronization target(s) failed", failed)
	}
	return results, nil
}
func cell(s string) string {
	s = html.EscapeString(concise(s))
	return strings.ReplaceAll(s, "|", "&#124;")
}
func WriteSummary(w io.Writer, results []Result) error {
	if _, err := fmt.Fprintln(w, "## Docker Syncer\n\n| Source | Platform | Target | Status | Duration | Reason |\n|---|---|---|---|---|---|"); err != nil {
		return err
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %.3fs | %s |\n", cell(r.Source), cell(r.Platform), cell(r.Target), cell(r.Status), r.Duration.Seconds(), cell(r.Reason)); err != nil {
			return err
		}
	}
	return nil
}
