# Gofoom

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
| **Executable Size** | > 80 MB (with embedded JRE) | **~13.5 MB** | **-83% size** |
| **Startup Time** | 2 ~ 4 seconds | **< 80 milliseconds** | **Instantaneous** |
| **PAC Engine** | Heavy GraalVM JS (~50MB overhead) | Pure Go `goja` (< 2MB overhead) | **Zero CGO / Instant** |
| **Background Tray** | Clunky Swing, flashes on startup | Native Wails v3 System Tray | **Silent startup** |

---

## ✨ Features

- **Upstream Proxy Types Supported**:
  - `DIRECT`: Bypass, direct connection
  - `HTTP`: Upstream HTTP proxy with Basic, Digest, NTLM v1/v2, or Kerberos/SPNEGO authentication
  - `SOCKS5`: SOCKS version 5 with or without username/password
  - `SOCKS4`: SOCKS version 4 / 4a (domain name support)
  - `PAC`: Proxy Auto-Configuration via JavaScript evaluator (Netscape & Microsoft IPv6 extensions)
- **Windows Integrated Authentication (SSO)**:
  - Uses native Windows SSPI (`secur32.dll`) via `alexbrainman/sspi`.
  - Pass corporate proxies without entering your domain password!
- **Dual Running Modes**:
  - **GUI Mode**: System Tray resident + modern dark dashboard + live traffic log viewer.
  - **Headless Daemon Mode**: Runs silently as a background service.
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

### 2. Using the CLI (`foomcli.bat`)
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
- [Go 1.22+](https://go.dev/dl/)
- Node.js & npm (optional, prebuilt `frontend/dist` is already included)

### Compile
```powershell
# Build development executable
go build -o bin/gofoom_dev.exe ./cmd/gofoom

# Build production silent executable (No black console window, icon embedded)
go build -ldflags "-w -s -H windowsgui" -o bin/gofoom.exe ./cmd/gofoom
```

### Run Tests
```powershell
go test -v ./pkg/proxy
go test -v ./pkg/auth
go test -v ./pkg/pac
go test -v ./pkg/api
```

---

## 📄 License
Apache License 2.0.
