// Command listtt serves a local dashboard of running Claude Code sessions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/nya1/listtt/internal/dashboard"
	"github.com/nya1/listtt/internal/focus"
	"github.com/nya1/listtt/internal/recap"
	"github.com/nya1/listtt/internal/sessions"
	"github.com/nya1/listtt/internal/store"
	"github.com/nya1/listtt/internal/web"
)

const pollInterval = 3 * time.Second

// version is set at build time via -ldflags "-X main.version=...".
// When installed via `go install @latest`, Go embeds the module version via
// runtime/debug.ReadBuildInfo, so we fall back to that when version is still "dev".
var version = "dev"

func init() {
	if version != "" && version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

const banner = `█     █████  ████ █████ █████ █████
█       █   █       █     █     █
█       █    ███    █     █     █
█       █       █   █     █     █
█████ █████ ████    █     █     █`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "listtt:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("listtt", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:7777", "listen address; the host must be 127.0.0.1 or localhost")
	openFlag := flags.Bool("open", false, "open the dashboard in the default browser")
	versionFlag := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *versionFlag {
		fmt.Println(version) // GNU-style: --version ignores other flags/args
		return nil
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	host, port, err := validateAddr(*addr)
	if err != nil {
		return err
	}

	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	file := store.File{Path: path}
	data, err := file.Load()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("address %s in use, try --addr", *addr)
		}
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dash := dashboard.New(data, file, focus.New(), recap.NewReader(), time.Now)
	if home, err := os.UserHomeDir(); err == nil {
		dash.Home = home
	}
	go pollLoop(ctx, sessions.NewSource(), dash, pollInterval)

	srv := &http.Server{
		Handler:           web.NewHandler(dash, port),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: /events is a long-lived stream.
		// BaseContext cancels open /events streams on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	url := "http://" + net.JoinHostPort(host, port) + "/"
	fmt.Println(banner)
	log.Printf("listtt %s: dashboard at %s (store: %s)", version, url, path)
	if *openFlag {
		if err := openBrowser(url); err != nil {
			log.Printf("listtt: could not open the browser: %v", err)
		}
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// validateAddr allows only loopback hosts, because the dashboard has no authentication.
func validateAddr(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return "", "", fmt.Errorf("--addr host must be 127.0.0.1 or localhost, got %q", host)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("invalid --addr port %q", port)
	}
	return host, port, nil
}

// pollLoop polls without overlap: the next poll starts `every` after the previous one finished.
func pollLoop(ctx context.Context, src *sessions.Source, dash *dashboard.Dashboard, every time.Duration) {
	for {
		list, err := src.List(ctx)
		if ctx.Err() != nil {
			return
		}
		dash.ApplyPoll(list, err, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func openBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the child process
	return nil
}
