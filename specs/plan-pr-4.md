# Logging: terminal output, parseable format, new-client highlighting, verbosity levels

Rhyolite logs requests, auth failures, tool calls and start/stop to `rhyolite.log`, but nothing reaches the terminal. This PR makes logging useful to someone watching the server run, in four steps.

## 1. Echo log lines to the terminal (done)

`logfile.Writer` gets an optional echo writer (`SetEcho`). `Printf` formats each line once and writes the identical bytes (same timestamp, same text) to the file and to the echo writer under the same mutex, so lines never interleave. `Write` echoes too. `main.go` sets the echo to stdout after the banner is printed, before the `start` line, so the banner stays at the top. Tests: `internal/logfile` (echo equals file) and `cmd/rhyolite` (log lines appear on stdout after the banner).

## 2. Parseable log format (done)

Every line is strict logfmt: `ts=<RFC3339> event=<name>` followed by `key=value` pairs in a fixed order. Values that are empty or contain a space, `=`, `"` or a control character are quoted with `strconv.Quote`; all others are bare. Events: `start`, `stop`, `http`, `auth_fail`, `tool` (keys documented in the README). The formatter is `logfile.Format`; `logfile.Writer.Log(event, kv...)` replaced `Printf`, and the server's `Logger` interface is now `Log(event string, kv ...any)`. A `time.Duration` value is rendered as integer milliseconds (`dur_ms`). Tool errors keep the 200-character truncation and appear as an extra `error` key. Tests: formatter edge cases and a round trip through a small logfmt parser.

## 3. Highlight new clients (done)

`bearerAuth` consults an in-memory `clientTracker` (reset on restart, capped at 10,000 IPs, after which nothing new is recorded or announced). The first request from an unseen IP logs `event=client_new ip=<ip> auth=ok|fail` before its `auth_fail`/`tool`/`http` lines. Requests rejected earlier by the origin guard are not announced. `logfile.Writer.SetEcho(out, highlight...)` wraps highlighted events in ANSI bold on the echo only when `out` is a terminal (`os.File` with `ModeCharDevice`); the log file stays plain.

## 4. Verbosity levels (done)

`--verbosity none|basic|verbose` flag, default `verbose`. `none` prints nothing on the terminal, `basic` prints setup info only (today's banner), `verbose` prints setup info plus log lines.

Implemented in `run`: an invalid value is an error naming the valid ones; `none` swaps stdout for `io.Discard` after `--version` handling (errors still go to stderr); the echo is only set in `verbose`. The log file is written in every mode.
