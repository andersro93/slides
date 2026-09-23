// Command slides serves the presentations. One binary, a few modes picked by
// argv[1]:
//
//	slides                     serve (default)
//	slides serve               serve $DECKS_DIR on $PORT (3000)
//	slides validate [dir]      check a presentations folder (default $DECKS_DIR, else .)
//	slides healthcheck         GET /healthz on localhost (the image has no curl)
//	slides version
//
// The frontend is embedded; the presentations are not. They are read from
// DECKS_DIR (a mounted folder), watched, and reloaded without a restart.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/andersro93/slides/server/internal/buildinfo"
	"github.com/andersro93/slides/server/internal/config"
	"github.com/andersro93/slides/server/internal/decks"
	"github.com/andersro93/slides/server/internal/embedded"
	"github.com/andersro93/slides/server/internal/remote"
	"github.com/andersro93/slides/server/internal/web"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "validate":
		dir := os.Getenv("DECKS_DIR")
		if dir == "" {
			dir = "."
		}
		if len(args) > 0 {
			dir = args[0]
		}
		err = validate(dir)
	case "healthcheck":
		err = healthcheck()
	case "version":
		fmt.Println(buildinfo.Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (serve, validate, healthcheck, version)\n", cmd)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	app := embedded.App()
	if cfg.AppDir != "" {
		app = os.DirFS(cfg.AppDir)
	}

	source, err := decks.NewSource(os.DirFS(cfg.DecksDir), decks.Options{IncludeDrafts: cfg.Dev}, log)
	if err != nil {
		return err
	}

	hub := remote.NewHub(remote.Options{Logger: log})
	opts := web.Options{
		App:            app,
		Library:        source.Library,
		Hub:            hub,
		ClientIPHeader: cfg.ClientIPHeader,
		Dev:            cfg.Dev,
		Logger:         log,
	}
	if cfg.Dev {
		opts.Problems = source.Problems
	}
	srv, err := web.New(opts)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go hub.Run(ctx)

	// SIGHUP: re-read the presentations now (e.g. from a deploy hook after
	// syncing the folder) instead of waiting for the next poll.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	poke := make(chan struct{}, 1)
	go func() {
		for range hup {
			select {
			case poke <- struct{}{}:
			default:
			}
		}
	}()
	go source.Watch(ctx, cfg.ReloadInterval, poke)

	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr(), "version", buildinfo.Version,
			"decks", cfg.DecksDir, "reload", cfg.ReloadInterval, "dev", cfg.Dev)
		errc <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Hijacked WebSockets are not tracked by http.Server; tell them to
	// reconnect (to the next instance) explicitly.
	hub.Close()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func validate(dir string) error {
	lib, err := decks.Load(os.DirFS(dir), decks.Options{IncludeDrafts: true})
	if err != nil {
		return err
	}
	printDecks(lib)
	fmt.Printf("\n%d presentation(s) OK\n", len(lib.All()))
	return nil
}

func printDecks(lib *decks.Library) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, d := range lib.All() {
		flags := string(d.Format)
		if d.Private {
			flags += ",private"
		}
		if d.Draft {
			flags += ",draft"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t(%s)\n", d.Code, flags, d.Title, d.Dir)
	}
	_ = tw.Flush()
}

func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}
