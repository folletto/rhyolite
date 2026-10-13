// Command rhyolite serves a folder of Markdown files as an MCP server over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/folletto/rhyolite/internal/config"
	"github.com/folletto/rhyolite/internal/logfile"
	"github.com/folletto/rhyolite/internal/server"
	"github.com/folletto/rhyolite/internal/vault"
)

// Set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "rhyolite:", err)
		os.Exit(1)
	}
}

const usage = `Usage: rhyolite [--port N] [--verbosity none|basic|verbose] [folder]

Serves the Markdown files in folder (default: the current folder) as an MCP
server over HTTP. No file outside the folder is ever accessed.
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("rhyolite", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage); fs.PrintDefaults() }
	showVersion := fs.Bool("version", false, "print the version and exit")
	port := fs.Int("port", -1, "listen port, overriding rhyolite.json (0 picks a free port)")
	verbosity := fs.String("verbosity", "verbose", "terminal output: none, basic (setup info only) or verbose (setup info and log lines)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "rhyolite", version)
		return nil
	}
	switch *verbosity {
	case "none":
		stdout = io.Discard
	case "basic", "verbose":
	default:
		return fmt.Errorf("invalid --verbosity %q: use none, basic or verbose", *verbosity)
	}
	if fs.NArg() > 1 {
		return errors.New("at most one folder argument is accepted")
	}
	folder := "."
	if fs.NArg() == 1 {
		folder = fs.Arg(0)
	}
	dir, err := filepath.Abs(folder)
	if err != nil {
		return err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a folder", folder)
	}

	cfg, status, err := config.Load(dir)
	if err != nil {
		return err
	}
	if status == config.Created {
		fmt.Fprintln(stdout, "▴ Creating rhyolite.json")
	} else {
		fmt.Fprintln(stdout, "▴ Reading rhyolite.json")
	}
	if *port >= 0 {
		cfg.Port = *port
	}

	logw, err := logfile.New(dir)
	if err != nil {
		return fmt.Errorf("cannot open rhyolite.log: %w", err)
	}
	defer logw.Close()

	v, err := vault.Open(dir, nil)
	if err != nil {
		return err
	}
	defer v.Close()

	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return err
	}
	actual := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{
		Handler:           server.New(server.Options{Vault: v, Token: cfg.Token, Log: logw, Version: version}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          nil,
	}

	printBanner(stdout, dir, cfg, actual)
	if *verbosity == "verbose" {
		logw.SetEcho(stdout, "client_new")
	}
	logw.Log("start", "version", version, "vault", filepath.Base(dir), "listen", ln.Addr())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	fmt.Fprintln(stdout, "\n▴ Stopping")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(sctx)
	logw.Log("stop")
	return err
}
