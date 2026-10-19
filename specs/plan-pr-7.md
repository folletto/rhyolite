# LAN or local-only access

Today the CLI always serves the whole network: `rhyolite.json` defaults `host` to `0.0.0.0`, `main.go` listens on that wildcard address, and every connection that presents the bearer token is served. The only way to restrict it is to set `host` to `127.0.0.1`, which has two problems:

- the banner then prints `http://127.0.0.1:6270/mcp` instead of `http://Studio.local:6270/mcp`, so every client config has to change when switching between the two modes;
- `Studio.local` can resolve to the machine's LAN addresses (IPv4 and IPv6), not only to loopback, so a client still configured with the local name may get "connection refused".

This PR adds an explicit switch between serving the LAN and serving only the machine itself, while the advertised URL (`http://Studio.local:6270/mcp`) stays the same in both modes.

## 1. Goals and non-goals

**Goals**
- A setting, `access`, with two values: `lan` (today's behaviour, the default) and `local` (only connections from this machine are served).
- Settable in `rhyolite.json` and overridable per run with a CLI flag, like `port`.
- In `local` mode the banner and both `mcp.json` snippets print exactly the same URL as in `lan` mode, so clients never need reconfiguring when the mode changes.
- A connection from another machine in `local` mode is refused before any HTTP is read, and logged.

**Non-goals**
- No change to `host`. It remains the bind address for advanced setups; `access` is the user-facing switch.
- No change to the macOS app in this PR (it keeps passing only the vault path). A toggle in its settings that passes `--access local` is a natural follow-up.
- No TLS changes.

## 2. Setting

- `config.Config` gets `Access string \`json:"access,omitempty"\``. Empty means `lan`. It is not written by default, so existing `rhyolite.json` files are not rewritten. `Load` rejects any value other than `""`, `lan` or `local` with an error naming the field, like `advertise`.
- New flag `--access lan|local` in `cmd/rhyolite/main.go`, overriding the file for this run (same pattern as `--port`; default `""` = use the file). An invalid value is an error, like `--verbosity`.
- Usage line becomes `rhyolite [--port N] [--access lan|local] [--verbosity none|basic|verbose] [folder]`.

## 3. How `local` is enforced

The listener keeps binding the configured `host` (wildcard by default). Binding to `127.0.0.1` instead is rejected as the mechanism, because the local name may resolve to the LAN IPs and the URL must keep working.

Instead, connections are filtered by where they come from. A connection is local when its remote IP is loopback, or equals the connection's local IP: when a process on this machine connects to one of the machine's own addresses (e.g. via `Studio.local` → `192.168.1.20`, or its IPv6 address), the kernel uses that same address as the source. A remote host cannot complete a TCP handshake with a forged source equal to our own address. IPv6 zones are ignored in the comparison; IPv4-mapped IPv6 addresses are normalised with `To16`/`Equal`.

Where: a small listener wrapper, `server.LocalOnly(ln net.Listener, log Logger) net.Listener`, in `internal/server/` (new file `listener.go`). Its `Accept` closes non-local connections immediately and keeps accepting; the HTTP server never sees them, so no headers or token from a LAN client are parsed. Each refused IP is logged once (`event=refused ip=…`, reusing the `clientTracker` dedupe and its cap) so a chatty LAN client can't flood `rhyolite.log`. `main.go` wraps the listener only when access is `local`.

Alternative considered: an HTTP middleware answering `403 Forbidden: local connections only`. It gives a clearer client-side error but reads the request first; the listener-level drop is the stronger guarantee for "don't serve the LAN". (Open question for review: switch to 403 if a readable error matters more.)

Hardening that comes for free: in `local` mode every connection is from this machine, so `hostGuard`'s Host check (today applied only to connections arriving on loopback) is applied to all connections. `server.Options` gets `LocalOnly bool`; `hostGuard` checks the Host header when the local address is loopback **or** `LocalOnly` is set. This closes DNS rebinding through the LAN address for same-machine browsers, on top of `originGuard`.

## 4. Banner and log

- `hostsFor` is unchanged and does not look at `access`: the same hosts in the same order in both modes, so the snippet URL is identical. The LAN IP fallbacks keep working from this machine too.
- `● Listening` and the `on:`/`or:` lines are unchanged (the macOS app relies on `● Listening`). After the URL lines a new line states the mode, in both modes: `› Access: local + LAN` for `lan`, `› Access: local only` for `local`:
  ```
  ● Listening
    on: http://Studio.local:6270/mcp
    or: http://192.168.1.20:6270/mcp
  › Access: local only
  › Log: rhyolite.log
  ```
- The `start` log line gains `access=lan|local`.

## 5. Tests

- `internal/config`: `access` round-trips; invalid values are rejected; a file without it is not rewritten.
- `internal/server` (`listener_test.go`): `LocalOnly` with fake conns (stub `RemoteAddr`/`LocalAddr`): loopback v4/v6 accepted; remote == local (v4, v6, v4-mapped) accepted; other IP closed and logged once; Accept continues after a refusal; listener errors propagate.
- `internal/server`: `hostGuard` with `LocalOnly` refuses a foreign Host on a non-loopback local address, accepts the local names.
- `cmd/rhyolite`: `--access local` starts, serves a request over `127.0.0.1` (and `[::1]` where available); the banner URLs equal those of a `lan` run with the same config; the banner shows `› Access: local only` with `local` and `› Access: local + LAN` with `lan`; `--access bogus` and `"access": "bogus"` are errors; the flag overrides the file.
- An end-to-end refusal needs a second host, so it is covered by the unit test only.

## 6. Docs

- `README.md`: document `--access lan|local` and `"access"` next to `--port` and `advertise`; note that the URL is the same in both modes; extend the security note (in `local` mode, other machines are refused at connect time, and the Host check applies to every connection).
- `AGENTS.md`: run line gains `[--access lan|local]`.
