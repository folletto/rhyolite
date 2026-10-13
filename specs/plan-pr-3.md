# Logging: terminal output, parseable format, new-client highlighting, verbosity levels

Rhyolite logs requests, auth failures, tool calls and start/stop to `rhyolite.log`, but nothing reaches the terminal. This PR makes logging useful to someone watching the server run, in four steps.

## 1. Echo log lines to the terminal (done)

`logfile.Writer` gets an optional echo writer (`SetEcho`). `Printf` formats each line once and writes the identical bytes (same timestamp, same text) to the file and to the echo writer under the same mutex, so lines never interleave. `Write` echoes too. `main.go` sets the echo to stdout after the banner is printed, before the `start` line, so the banner stays at the top. Tests: `internal/logfile` (echo equals file) and `cmd/rhyolite` (log lines appear on stdout after the banner).

## 2. Parseable log format (planned)

Make the log format easy to parse by an algorithm.

## 3. Highlight new clients (planned)

Highlight when a new client IP connects.

## 4. Verbosity levels (planned)

`--verbosity none|basic|verbose` flag, default `verbose`. `none` prints nothing on the terminal, `basic` prints setup info only (today's banner), `verbose` prints setup info plus log lines.
