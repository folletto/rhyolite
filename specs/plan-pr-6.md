# Connect by local name instead of IP

The startup banner and the ready-to-paste `mcp.json` snippets use the machine's LAN IP (`http://192.168.1.20:6270/mcp`). On most home networks that IP is handed out by DHCP and can change, which silently breaks every client config. This PR makes the CLI advertise the machine's mDNS name (`http://Studio.local:6270/mcp`), which the operating system keeps pointing at the current IP.

## 1. Goals and non-goals

**Goals**
- The banner's first URL, and the URL inside both `mcp.json` snippets, use the local name when one is available.
- The LAN IPs stay listed as fallbacks, for networks where mDNS doesn't work.
- An optional `rhyolite.json` setting overrides the advertised name (e.g. a router name like `studio.lan`, or an IP to keep the old behaviour).
- Everything stays in the CLI. The macOS app is unchanged: it keeps relying only on `● Listening`.

**Non-goals**
- Rhyolite does not announce itself over mDNS (no Bonjour service registration, no new dependency). The operating system already answers for `<name>.local`; Rhyolite only prints that name.
- No startup check that the name resolves. Resolving `.local` from Go without cgo is unreliable across platforms, and a failed check would only produce noise; the IP fallbacks cover it.
- No HTTPS changes (discussed separately).

## 2. Where the name comes from (done)

A new function `localName() string` in `cmd/rhyolite/`, one build-tagged file per platform. It returns a bare name with `.local` appended, or `""` when the platform has no reliable mDNS name:

| Platform | Source | Notes |
|---|---|---|
| macOS (`localname_darwin.go`) | `scutil --get LocalHostName` (absolute path `/usr/sbin/scutil`, 1 s timeout) | This is the Bonjour name shown in System Settings → General → Sharing. `os.Hostname()` is not used: routers often override it with DHCP-supplied names like `192-168-1-20.home`. |
| Linux (`localname_linux.go`) | first label of `os.Hostname()` | Only when Avahi is running (`/run/avahi-daemon/pid` exists); otherwise `""`, because nothing would answer for the name. |
| Windows (`localname_windows.go`) | `os.Hostname()` | Windows 10 and later answer mDNS for the computer name. |
| Others (`localname_other.go`) | none | Returns `""`. |

The result is validated as a DNS label (letters, digits, `-`, at most 63 characters); anything else is discarded and the CLI falls back to IPs. The exec and file checks sit behind package-level function variables so tests can stub them on any OS.

## 3. Which hosts the banner prints (done)

`hostsFor` (in `banner.go`) becomes `hostsFor(cfg, localName, lanIPs)`:

1. If `rhyolite.json` sets `"advertise"`, that value comes first.
2. Otherwise, if the server listens on all interfaces (`host` empty, `0.0.0.0` or `::`) and `localName()` is non-empty, the local name comes first.
3. Then the LAN IPs, private first, as today.
4. If the server listens on a specific address (e.g. `127.0.0.1`), only that address is printed, as today. The local name is skipped because it wouldn't reach the listener.
5. With nothing found, `localhost`, as today.

Duplicates are removed (e.g. `advertise` set to one of the IPs). The snippets use the first host.

Banner:

```
● Listening
  on: http://Studio.local:6270/mcp
  or: http://192.168.1.20:6270/mcp
› Log: rhyolite.log
```

The `● Listening` line is unchanged, so the macOS app needs nothing.

## 4. Configuration (done)

`config.Config` gets `Advertise string \`json:"advertise,omitempty"\``. It is optional and never written by default, so existing `rhyolite.json` files are not rewritten. It holds a host only (no scheme, no port), and `Load` rejects a value containing `/`, `:` (other than an IPv6 literal) or spaces with an error naming the field.

## 5. IPv6 (done)

A `.local` name often resolves to the Mac's IPv6 addresses as well as IPv4. No listener change is needed: Go's `net.Listen("tcp", "0.0.0.0:port")` treats the unspecified address as a wildcard and opens a dual-stack socket on macOS, Linux and Windows, so IPv6 clients are accepted. A test locks this in by dialling `[::1]` against a server started with the default host (skipped where IPv6 is unavailable).

## 6. Tests (done)

- `localName`: stubbed `scutil` output (trailing newline trimmed, error or timeout → `""`, invalid label → `""`), Avahi present/absent on Linux, hostname with a domain suffix → first label only.
- `hostsFor`: name first then IPs; `advertise` wins and is de-duplicated; specific listen host → that host only; nothing found → `localhost`.
- Banner: both snippets contain the first host's URL; the existing end-to-end test keeps connecting through `127.0.0.1` (it already swaps the host).
- Config: `advertise` round-trips, is omitted when empty, invalid values are rejected; a file without it is not rewritten.
- Dual-stack listener test (§5).

## 7. Documentation (done)

- `README.md`: the quick-start banner shows the local name; a short paragraph says where the name comes from, that IPs remain as fallbacks (guest Wi-Fi and networks with client isolation block mDNS), that macOS may rename the machine to `Name-2.local` on a conflict, and how to use `"advertise"`.
- `AGENTS.md`: no change needed.
