# OnePlus Router desktop fallback launchers

The router dashboard now includes an authenticated browser terminal and
browser-based scrcpy control. The launchers below remain as a recovery path if
the embedded WebSocket interface is unavailable.

The router dashboard exposes two Windows-only links:

- `op7router-scrcpy://start` connects wireless ADB to
  `192.168.2.1:5555` and starts the bundled scrcpy.
- `op7router-ssh://start` opens an SSH terminal to
  `192.168.2.1:8022` using the current user's SSH key.

Run `install-web-launchers.ps1` once for each Windows account that should use
the dashboard buttons. It registers both URL protocols under HKCU, so it does
not require administrator privileges. Browsers normally ask for confirmation
the first time an external protocol is opened.

`enable-router-ping.ps1` creates a narrowly scoped Windows Firewall rule that
allows ICMPv4 echo requests only from `192.168.2.0/24`; run it elevated.
