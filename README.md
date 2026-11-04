# Gofoom

[![Version](https://img.shields.io/badge/version-1.2.1-blue.svg)](https://github.com)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Wails v3](https://img.shields.io/badge/GUI-Wails%20v3-DF0000?style=flat)](https://v3.wails.io)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey)](https://github.com)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Gofoom** is a blazing-fast, ultra-lightweight HTTP(S) proxy server facade for NTLM, Kerberos, SOCKS, and Proxy Auto-Configuration (PAC) proxies. It is a 1:1 modern rewrite of [Winfoom](https://github.com/ecovaci/winfoom) in Go and Wails v3.

It sits between your applications and corporate authenticated proxies, seamlessly handling complex handshakes and Integrated Windows Authentication (SSO), so your tools (Git, curl, Docker, npm, browsers) only deal with a plain unauthenticated local proxy on port `3129`.

---

## 🚀 Key Advantages Over Original Java Winfoom

| Metric | Winfoom (Java 25 + Spring Boot) | **Gofoom (Go + Wails v3)** | Improvement |
| :--- | :--- | :--- | :--- |
| **Runtime Dependencies** | Requires Java 25 JRE (80MB+ bundle) | **Zero (Single standalone .exe)** | **100% portable** |
| **Idle Memory (RAM)** | 150 MB ~ 300 MB+ | **20 MB ~ 35 MB** | **-85% memory** |
| **Executable Size** | > 80 MB (with embedded JRE) | **~13.6 MB** | **-83% size** |
| **Startup Time** | 2 ~ 4 seconds | **< 80 milliseconds** | **Instantaneous** |
| **PAC Engine** | Heavy GraalVM JS (~50MB overhead) | Pure Go `goja` (< 2MB overhead) | **Zero CGO / Instant** |
| **Background Tray** | Clunky Swing, flashes on startup | Native Wails v3 System Tray | **Silent startup** |

---

## ✨ Features

- **Upstream Proxy Types Supported**:
  - `DIRECT`: Bypass, direct connection with smart in-memory DNS caching.
  - `HTTP`: Upstream HTTP proxy with Basic, Digest, NTLM v1/v2, or Kerberos/SPNEGO authentication.
  - `SOCKS5`: SOCKS version 5 with optional RFC 1929 username/password authentication.
  - `SOCKS4`: SOCKS version 4 / 4a (domain name support).
  - `PAC`: Proxy Auto-Configuration with automated blacklist timeout failover (`blacklistTimeout`) and dynamic multi-candidate failover, supporting NTLM/Kerberos/Basic upstream proxy authentication and one-click blacklist reset.
- **Windows Integrated Authentication (SSO)**:
  - Uses native Windows SSPI (`secur32.dll`) via `alexbrainman/sspi`.
  - Pass corporate proxies without entering your domain password!
- **DNS Caching & Connection Resilience**:
  - In-memory thread-safe DNS cache (5-minute TTL) with stale fallback. Eliminates `ERR_TUNNEL_CONNECTION_FAILED` during burst parallel browser requests.
  - Automatic transient dial retry for momentary network packet drops.
- **LAN Sharing (`Allow LAN`)**:
  - Option to bind to `0.0.0.0` to share your proxy facade with other computers (e.g. Computer B, mobile devices, VMs) on your local network.
  - Intelligent internal loopback routing: requests to your machine's IP on port 9999 are served directly without outbound network friction.
- **Multi-Instance Isolation**:
  - Folder-hashed single-instance mutex and dynamic API port failover (`9999` -> `10000+`). Run multiple instances with distinct configurations simultaneously from different directories!
- **Independent Autostart & Window Minimization**:
  - Fully decoupled settings for "Auto-start proxy on app launch", "Start minimized to system tray", and "Launch at Windows startup".
- **Dual Running Modes & REST API**:
  - **GUI Mode**: System Tray resident + modern dark dashboard + live traffic log viewer.
  - **Headless Daemon Mode**: Runs silently as a background service.
  - **Interactive Web API Dashboard**: Visiting `http://localhost:9999/` serves a clean management dashboard with live statistics and API documentation.
- **foomcli Compatibility**:
  - Exposes local management REST API on `:9999` compatible with original `foomcli` scripts.
- **System Proxy Auto-Detection**:
  - Automatically detects Windows IE / system proxy settings.

---

## 📦 Getting Started

### 1. Run Prebuilt Binary
Directly run the compiled executable in `bin/`:
```powershell
# Launch in GUI & System Tray mode
.\bin\gofoom.exe

# Or start directly minimized to System Tray
.\bin\gofoom.exe -minimized

# Or run in headless daemon mode
.\bin\gofoom.exe -headless
```

### 2. Sharing Proxy on Local Network (LAN Mode)
1. On **Computer A**, check the **`Allow LAN connections (0.0.0.0)`** checkbox next to the Local Proxy Port on the main screen and click **Save Settings**.
2. Allow Gofoom in Windows Firewall (run once as Administrator if blocked):
   ```powershell
   New-NetFirewallRule -DisplayName "Gofoom Proxy Facade" -Direction Inbound -LocalPort 3129,9999 -Protocol TCP -Action Allow
   ```
3. On **Computer B**, set your proxy to `Computer_A_IP:3129`. You can now browse through Computer A, and visit `http://Computer_A_IP:9999/` to view the Gofoom API dashboard.

### 3. Using the CLI (`foomcli.bat`)
When running in headless or GUI mode, Gofoom exposes a local API on `http://127.0.0.1:9999`:
```powershell
# Check status
.\foomcli.bat status

# Start / Stop proxy
.\foomcli.bat start
.\foomcli.bat stop

# View current configuration
.\foomcli.bat config

# Load configuration from JSON
.\foomcli.bat config -f http_config.json

# Auto-detect system proxy
.\foomcli.bat autodetect

# Validate connection
.\foomcli.bat validate

# Shutdown Gofoom
.\foomcli.bat shutdown
```

---

## 🛠️ Build from Source

### Prerequisites
- [Go 1.22+](https://go.dev/dl/) (Go 1.27.3+ recommended)
- Node.js & npm (optional, prebuilt `frontend/dist` is already included)

### Compile
Use the automated build script (synchronizes version metadata and Windows PE executable resources automatically):
```powershell
# Build production silent executable (No console window, icon & PE version metadata embedded)
.\build.ps1

# Build development executable (with console window enabled for debugging)
.\build.ps1 -Dev
```

Or manually with standard Go toolchain:
```powershell
# 1. Synchronize Windows PE metadata
go generate ./...

# 2. Build production executable
go build -ldflags "-s -w -H=windowsgui" -o bin/gofoom.exe ./cmd/gofoom
```

### Run Tests
```powershell
go test -v ./...
```

---

## 📄 License
Apache License 2.0.
