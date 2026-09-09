package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Develata/docker-syncer/internal/syncer"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "docker-syncer:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out, errout io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(out, "docker-syncer sync [--image REF | --file images.txt] [options]\ndocker-syncer config [--config syncer.json] [--mode MODE]\nUse sync --help for all options. Transport requires Skopeo; dry-run is offline.")
		return nil
	}
	if args[0] != "sync" && args[0] != "config" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errout)
	config := fs.String("config", "", "strict JSON configuration file (optional syncer.json by default)")
	mode := fs.String("mode", "", "aliyun, ghcr, double, none")
	platform := fs.String("platform", "", "OS/architecture[/variant] or all")
	image := fs.String("image", "", "one source image (exclusive with --file)")
	file := fs.String("file", "", "image list (default images.txt)")
	target := fs.String("target-name", "", "explicit relative repository:tag for --image")
	force := fs.Bool("force", false, "copy even if target manifest digest matches")
	dry := fs.Bool("dry-run", false, "offline plan only; no authentication or registry requests")
	auth := fs.String("authfile", "", "Skopeo auth JSON (otherwise isolated empty credentials)")
	summary := fs.String("summary", "", "write Markdown run summary to this file")
	retries := fs.Int("retries", 0, "maximum retries per transient operation (0..5)")
	timeout := fs.String("timeout", "", "total timeout per metadata/copy operation")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments; use --image")
	}
	c, err := syncer.LoadConfig(*config, os.LookupEnv)
	if err != nil {
		return err
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if args[0] == "config" {
		for name := range explicit {
			if name != "config" && name != "mode" {
				return fmt.Errorf("--%s is not valid for config", name)
			}
		}
	}
	if explicit["mode"] {
		c.Mode = *mode
	}
	if explicit["platform"] {
		c.Platform = *platform
	}
	if explicit["retries"] {
		c.Retries = *retries
	}
	if explicit["timeout"] {
		c.Timeout = *timeout
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if args[0] == "config" {
		fmt.Fprintln(out, c.Mode)
		return nil
	}
	prefixes, err := c.Targets()
	if err != nil {
		return err
	}
	p, err := syncer.ParsePlatform(c.Platform)
	if err != nil {
		return err
	}
	if *image != "" && *file != "" {
		return errors.New("--image and --file are mutually exclusive")
	}
	if *target != "" && *image == "" {
		return errors.New("--target-name requires --image")
	}
	var entries []syncer.Entry
	if *image != "" {
		src, err := syncer.ParseReference(*image)
		if err != nil {
			return err
		}
		entries = []syncer.Entry{{Source: src, Platform: p, Target: *target}}
	} else {
		name := *file
		if name == "" {
			name = "images.txt"
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		entries, err = syncer.ReadEntries(f, p)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	plans, err := syncer.BuildPlans(entries, prefixes)
	if err != nil {
		return err
	}
	var report *os.File
	if *summary != "" {
		report, err = os.OpenFile(*summary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		defer func() {
			if report != nil {
				_ = report.Close()
			}
		}()
	}
	authfile := *auth
	if !*dry && c.Mode != "none" {
		if authfile == "" {
			dir, err := os.MkdirTemp("", "docker-syncer-auth-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			authfile = filepath.Join(dir, "auth.json")
			if err := os.WriteFile(authfile, []byte(`{"auths":{}}`), 0600); err != nil {
				return err
			}
		} else {
			st, err := os.Stat(authfile)
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() {
				return errors.New("authfile must be a regular file")
			}
			if st.Mode().Perm()&0077 != 0 {
				return errors.New("authfile must not be accessible to group/others (chmod 600)")
			}
		}
	}
	duration, _ := time.ParseDuration(c.Timeout)
	transport := syncer.Skopeo{Authfile: authfile, Retries: c.Retries, Timeout: duration}
	results, runErr := syncer.Run(ctx, transport, plans, syncer.RunOptions{Force: *force, DryRun: *dry, Log: out})
	if report != nil {
		if err := syncer.WriteSummary(report, results); err != nil {
			return errors.Join(runErr, err)
		}
		if err := report.Close(); err != nil {
			return errors.Join(runErr, err)
		}
		report = nil
	}
	return runErr
}
