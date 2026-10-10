<p align="center">
  <img src="banner.png" alt="mittodrop banner" width="600" style="max-width: 100%; height: auto;">
</p>

<p align="center">
  <strong>Fast, End-to-End Encrypted, Peer-to-Peer File & Directory Transfer CLI</strong>
</p>

<p align="center">
  <a href="https://github.com/Nova-Stark/mittodrop-cli/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/go-1.27+-00ADD8.svg" alt="Go Version">
  <img src="https://img.shields.io/badge/platform-linux%20%7C%20macos%20%7C%20windows-lightgrey.svg" alt="Supported Platforms">
  <a href="https://github.com/Nova-Stark"><img src="https://img.shields.io/badge/author-Nova--Stark-orange.svg" alt="Author"></a>
</p>

---

## About

`mittodrop` is a high-performance, privacy-focused command-line tool built to transfer files and recursive directories securely between computers without friction.

Traditional file-sharing tools often require third-party cloud uploads, complex VPN setups, account registrations, or unencrypted local transfers. `mittodrop` eliminates these barriers:
- **Zero Cloud Storage**: Payloads stream directly between endpoints or through blind relay pipes with zero data retained on intermediate servers.
- **Zero-Knowledge Pairing**: Peers connect using simple, human-readable codephrases (e.g. `ocean-star-summit`), deriving mutual session keys via SPAKE2 so passwords never touch the wire.
- **Anywhere, Any Device**: Move data across local LANs, restrictive corporate NATs, mobile devices via web browser (LinkShare), or through your own self-hosted relay.
- **True Directory Streaming**: Directories stream as live TAR archives without creating temporary archive files on disk.

---

## Key Features

- **Zero-Knowledge Key Agreement**: Uses PAKE (Password-Authenticated Key Exchange) to establish session keys. Passphrases are never transmitted over the wire or stored on relay servers.
- **End-to-End Authenticated Encryption (AEAD)**: Stream-encrypts payloads in chunked frames using hardware-accelerated AES-256-GCM with unique 96-bit nonces per chunk.
- **Recursive Folder Streaming**: Packs directories into standard TAR streams on-the-fly and unpacks them live on the receiving host with zero temporary archive disk overhead.
- **Zip-Slip Defense**: Receiver sanitizes relative paths and enforces destination jail confinement to prevent path traversal attacks.
- **LinkShare (Browser-to-CLI Transfer)**:
  - Transfer files between any web browser (iPhone, Android, PC) and your terminal without installing extra software.
  - In-browser client-side Web Crypto SHA-256 checksumming and native Gzip `CompressionStream` compression.
- **Flexible Network Transports**:
  - **Direct P2P**: High-speed direct TCP transfers with automatic ephemeral port fallback and UPnP IGD gateway port mapping.
  - **Shout Mode**: Zero-configuration LAN discovery and signaling.
  - **Otin / Relay Mode**: WebSocket-based rendezvous pipe for traversing restrictive firewalls, cellular networks, and symmetric NATs.
  - **Self-Hostable Relay Server**: Run your own privacy-first rendezvous relay with `mittodrop relay serve`.
- **Integrity Validation**: Verifies full payload SHA-256 checksums before finalizing writes via atomic staging files.

---

## Architecture & How It Works

`mittodrop` pairs endpoints using ephemeral Password-Authenticated Key Exchange (SPAKE2), derives mutually authenticated 256-bit AES-GCM session keys, and streams chunked payloads with adaptive compression (Zstandard & browser Gzip) and full-stream SHA-256 integrity verification.

For complete sequence diagrams, connection state machines, and transport flowcharts, see the **[System Architecture Guide](docs/ARCHITECTURE.md)** and the **[Cryptographic Specification](docs/SPECIFICATION.md)**.

---

## Security Model

1. **Ephemeral Key Derivation**: Session keys are derived from high-entropy SPAKE2 curves combined with shared codephrases. An eavesdropper or malicious relay operator cannot decrypt the payload or perform offline dictionary attacks.
2. **Authenticated AEAD Framing**: Every payload chunk contains a 128-bit authentication tag. Corrupted, modified, or out-of-order chunks are rejected immediately before processing.
3. **Relay Blindness**: Rendezvous servers only route opaque binary coordinates. The relay never learns passwords, private keys, or plaintext data.
4. **Safe Path Normalization & Sandboxing**: Folder unpack operations reject absolute paths, volume prefixes, directory climbing tokens (`../`), or symlinks escaping the destination folder.
5. **Atomic File Staging**: Incoming single files are written to hidden temporary staging files and committed via atomic rename only upon full-stream SHA-256 verification.

---

## Installation

### Quick Install (Recommended)

#### Linux & macOS
```bash
curl -fsSL https://raw.githubusercontent.com/Nova-Stark/mittodrop-cli/master/install.sh | bash
```

#### Windows (PowerShell)
```powershell
irm https://raw.githubusercontent.com/Nova-Stark/mittodrop-cli/master/install.ps1 | iex
```

Once installed, `mitto` is immediately available from anywhere in your terminal.

---

### Install via Go

If you have Go installed, you can install `mitto` directly into your `$GOPATH/bin`:

```bash
go install github.com/Nova-Stark/mittodrop-cli/cmd/mittodrop@latest
```

### Building from Source

Prerequisites: **Go 1.24+**

```bash
# 1. Clone the repository
git clone https://github.com/Nova-Stark/mittodrop-cli.git
cd mittodrop-cli

# Option A: Build with Makefile (recommended)
make build

# Option B: Build directly using Go
go build -ldflags "-s -w" -o bin/mittodrop ./cmd/mittodrop

# Option C: Install directly into your GOPATH/bin using Go
go install ./cmd/mittodrop
```

The compiled binary will be placed in `bin/mittodrop` (or `bin/mittodrop.exe` on Windows). Verify the installation:

```bash
./bin/mittodrop version
```

---

## Transfer Methods: Why, When, & How

`mitto` offers five dedicated transfer methods tailored for different networking environments:

| Method | When to Use | Why Use It | How to Use |
| :--- | :--- | :--- | :--- |
| **Direct P2P (`direct`)** | Same local Wi-Fi/LAN, or direct WAN with public IP / port forwarding. | **Fastest transfer speed.** Streams direct TCP socket-to-socket without intermediaries. Features automatic port-hunting (`42201-42205` -> `:0`) and UPnP router forwarding. | Receiver runs `mitto direct rec -c <code>`. Sender runs `mitto direct send -a <ip:port> -c <code> <files/folders>`. |
| **Shout (`shout`)** | Fast LAN transfers between devices on the same subnet without typing IP addresses. | **Zero configuration.** Discovers peers automatically via local network multicast/broadcast beacons. | Receiver runs `mitto shout rec`. Sender runs `mitto shout send <files/folders>` or targets `mitto shout send -u <name> <files>`. |
| **LinkShare (`linkshare`)** | Sharing with mobile phones (iOS, Android) or laptops **without installing any app**. | **Zero install on receiver.** Serves an in-memory web portal with client-side Web Crypto SHA-256 and native browser Gzip compression. Also supports high-speed CLI-to-CLI batch uploads. | Host runs `mitto linkshare serve -p 8080`. Mobile/browser opens the URL and drags files. CLI upload runs `mitto linkshare send -u <url> <files>`. |
| **Otin Tunnel (`otin`)** | Connecting across strict NATs and firewalls using WireGuard/Tailcat peer mesh. | **P2P encrypted overlay.** Establishes an encrypted DERP WireGuard tunnel without exposing public ports. | Receiver runs `mitto otin rec -c <code>`. Sender runs `mitto otin send -a <tailcat-addr> -c <code> <files/folders>`. |
| **Relay Mode (`otin -r`)** | Cross-network transfers traversing symmetric NATs, cellular data, or corporate proxies where P2P tunnels fail. | **Blind relay pairing.** Pipes encrypted traffic through a lightweight rendezvous relay. The relay only sees opaque binary frames (zero-knowledge). | Receiver runs `mitto otin rec -r <relay:port> -c <code>`. Sender runs `mitto otin send -r <relay:port> -c <code> <files/folders>`. |

> 💡 *Want to run your own relay server? See **[Hosting a Private Relay Server](#hosting-a-private-relay-server)**.*

---

## Complete CLI Flag Reference

### Global Flags

| Flag | Long Flag | Description | Default | Example |
| :--- | :--- | :--- | :---: | :--- |
| `-v` | `--version`, `version` | Display version, commit hash, and build timestamp | — | `mitto version` |
| `-h` | `--help`, `help` | Show general help or command-specific usage | — | `mitto help` |

### Method 1: Direct P2P (`mitto direct`)

#### Receiver: `mitto direct rec` / `mitto manual rec`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-c` | `--code`, `--codephrase` | `<string>` | Preset 3-word PAKE codephrase (auto-generated if omitted) | *(Auto-generated)* |
| `-p` | `--port` | `<int>` | Preferred listening port (`0` to hunt free port in pool) | `0` |
| `-d` | `--dir`, `--output` | `<path>` | Destination directory to save incoming files | `.` |
| | `--no-upnp` | *(none)* | Disable automated UPnP router port forwarding | `false` |

#### Sender: `mitto direct send` / `mitto manual send`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-a` | `--addr`, `--address`, `-u`, `--target` | `<host:port>` | **(Required)** Target receiver address or comma-separated candidates | — |
| `-c` | `--code`, `--codephrase` | `<string>` | **(Required)** Shared 3-word PAKE codephrase | — |
| `-f` | `--file`, `--files` | `<path...>` | Paths to send (can also be supplied as trailing positional args) | — |

---

### Method 2: Shout LAN Discovery (`mitto shout` / `mitto send` / `mitto rec`)

#### Receiver: `mitto shout rec` / `mitto rec`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-d` | `--dir`, `--output` | `<path>` | Destination directory to save incoming files | `.` |
| `-p` | `--port` | `<int>` | Specific transfer port to bind | `0` *(Auto)* |
| `-t` | `--token` | `<string>` | Pre-shared authorization token (auto-generated if omitted) | *(Auto-generated)* |

#### Sender: `mitto shout send` / `mitto send`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-u` | `--user`, `--target` | `<string>` | Specific receiver name or IP to target (broadcasts if omitted) | *(All peers)* |
| `-t` | `--token` | `<string>` | Authorization token matching receiver | *(None)* |
| `-f` | `--file`, `--files` | `<path...>` | Paths to send (or trailing positional args) | — |

> [!TIP]
> When running `mitto send` without `-u`, an interactive prompt lists all discovered peers on the LAN. You can select multiple receivers using comma-separated numbers (e.g. `1, 3`), contiguous ranges (e.g. `1-4`), or simply press **Enter** to broadcast to all discovered receivers.

---

### Method 3: LinkShare Browser & CLI Portal (`mitto linkshare`)

#### Host / Web Server: `mitto linkshare serve`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-p` | `--port` | `<int>` | HTTP web portal listening port (`0` auto-selects free port) | `0` |
| `-d` | `--dir`, `--output` | `<path>` | Directory to store uploaded files from browsers/CLI | `.` |
| `-t` | `--token` | `<string>` | Required authorization token for access URL | *(Auto-generated)* |

#### CLI Uploader: `mitto linkshare send`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-u` | `--url`, `-ip` | `<url>` | Destination LinkShare URL (e.g. `http://192.168.1.5:8080?token=xyz`) | — |
| `-t` | `--token` | `<string>` | Access token (if not already embedded in `-u` query parameter) | — |
| `-c` | `--compress` | `<algorithm>`| Compression mode: `zstd`, `gzip`, or `none` | `zstd` |
| `-f` | `--files` | `<path...>` | Paths to upload (or trailing positional args) | — |
| | `-uf` | `<"url f1 f2">`| Combined destination URL and file bundle string | — |

> [!TIP]
> **LinkShare Token Authentication**:
> - **Web Browser**: Access tokens can be passed via the link (`?token=xyz`) or typed directly into the **Access Token** field in the browser UI. Entered tokens are saved in `sessionStorage` for convenient repeat transfers.
> - **CLI Sender**: If `-t` is omitted and the URL has no `?token=` parameter, `mitto linkshare send` will prompt for the access token interactively upon connecting to a token-protected receiver.

---

### Method 4: Otin Tunnel & Rendezvous Relay (`mitto otin`)

Otin supports two distinct transport modes:
1. **Tunnel Mode (Default)**: Uses WireGuard/Tailcat peer mesh directly via `-a <tailcat-addr>`.
2. **Relay Mode (`-r`)**: Routes through a rendezvous relay server via `-r <relay:port>` (bypassing symmetric NATs).

#### Receiver: `mitto otin rec`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-c` | `--code`, `--codephrase` | `<string>` | Shared 3-word PAKE codephrase (auto-generated if omitted) | *(Auto-generated)* |
| `-r` | `--relay` | `<host:port>` | Custom rendezvous relay server address | *(Default relay)* |
| | `--relay-pass` | `<string>` | Authorization password for private relay server | *(None)* |
| `-d` | `--dir`, `--output` | `<path>` | Destination directory to save incoming files | `.` |
| `-p` | `--port` | `<int>` | Virtual tunnel port | `42201` |

#### Sender: `mitto otin send`
| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-c` | `--code`, `--codephrase` | `<string>` | **(Required)** Shared 3-word PAKE codephrase | — |
| `-r` | `--relay` | `<host:port>` | Custom rendezvous relay server address | *(Default relay)* |
| | `--relay-pass`, `--relay-password` | `<string>` | Authorization password for private relay server | *(None)* |
| `-a` | `--addr`, `--address`, `-u`, `--target` | `<string>` | Explicit peer Tailcat address | *(Auto-paired)* |
| `-f` | `--file`, `--files` | `<path...>` | Paths to send (or trailing positional args) | — |

---

## Hosting a Private Relay Server (`mitto relay serve`)

Organizations and self-hosters can run their own lightweight, privacy-preserving rendezvous relay daemon on a VPS or internal server:

```bash
# Start a custom relay server on port 42201 with 30-minute room TTL
mitto relay serve -p 42201 --ttl 30m --max-rooms 1000

# Optionally protect with password authentication
mitto relay serve -p 42201 --pass "my-private-token"
```

Once running, clients can route their transfers through your relay by passing `-r <host:port>` (and `--relay-pass` if password-protected):
```bash
mitto otin rec -c ocean-star-summit -r relay.example.com:42201
mitto otin send -c ocean-star-summit -r relay.example.com:42201 ./my-folder
```

### Relay Server Configuration Flags

| Flag | Long Flag | Parameter | Description | Default |
| :--- | :--- | :--- | :--- | :---: |
| `-p` | `--port` | `<int>` | Port to bind relay server | `9007` |
| `-h` | `--host`, `--bind` | `<string>` | Host interface address to bind | `0.0.0.0` |
| | `--pass`, `--password` | `<string>` | Require password authentication from connecting clients | *(Open)* |
| | `--banner` | `<string>` | Custom banner text sent upon connection | `mittodrop-relay`|
| | `--ttl` | `<duration>` | Maximum lifetime for unused waiting rooms (e.g. `30m`, `1h`)| `30m` |
| | `--max-rooms` | `<int>` | Ceiling on concurrent pending rendezvous rooms | `1000` |
| | `--rate-limit` | `<int>` | Maximum connection requests per client IP within window | `60` |
| | `--rate-window` | `<duration>` | Sliding time window for rate limiting (e.g. `1m`, `10s`) | `1m` |

---

## Usage Guidelines: Do's and Don'ts

### Positional Arguments
- **Files & Folders**: In any `send` command, files and directories can be passed positionally at the end of the command or via `-f`:
  ```bash
  # Positional arguments (cleanest):
  mitto direct send -a 192.168.1.15:42201 -c ocean-star-summit ./file.txt ./my-folder

  # Explicit -f flag:
  mitto direct send -a 192.168.1.15:42201 -c ocean-star-summit -f ./file.txt
  ```

### Do's ✅
- **Do send folders directly**: You never need to tar, zip, or compress directories manually. Pass the folder path directly (`mitto direct send ... ./my-folder`). `mitto` packs and extracts the TAR stream on the fly.
- **Do quote multi-word codephrases**: If your shell treats dashes or spaces specially, wrap your codephrase in quotes: `-c "ocean-star-summit"`.
- **Do use LinkShare for non-technical recipients**: When sending to a colleague on a smartphone or tablet, run `mitto linkshare serve` and send them the local URL.
- **Do let the receiver pick the port**: Omit `-p` on `direct rec` so `mitto` can bind an unoccupied port automatically and avoid collisions.

### Don'ts ❌
- **Don't transmit codephrases over insecure channels**: The PAKE handshake ensures the network wire is secure, but you should share the 3-word codephrase with your recipient through a private channel.
- **Don't hardcode fixed ports in shared scripts**: Prefer default ephemeral port selection (`-p 0`) to prevent `address already in use` errors when multiple transfers occur simultaneously.
- **Don't use `mitto` for malicious purposes**: As defined in our [Security Policy](SECURITY.md), using `mitto` for malware development, command-and-control, or unauthorized data exfiltration is strictly prohibited.

---

## Contributing

Contributions, bug fixes, and suggestions are welcome! To maintain protocol integrity and cross-platform reliability, please review our contributor guidelines before submitting pull requests:

- 📖 Read the **[Contributing Guide](CONTRIBUTING.md)** for development setup, testing requirements, Conventional Commits formatting, and PR submission checklist.


---

## Security

`mittodrop` prioritizes cryptographic rigor and user privacy. If you discover a potential vulnerability or security flaw, please report it privately rather than opening public issues:

- 🛡️ Read our **[Security Policy](SECURITY.md)** for responsible disclosure procedures, supported releases, and our anti-malware Acceptable Use Policy.
- 🔒 Submit private vulnerability reports via [GitHub Security Advisories](https://github.com/Nova-Stark/mittodrop-cli/security/advisories).

---

## Disclaimer & Ethical Use

> [!WARNING]
> `mittodrop` is developed strictly for lawful administrative and personal file transfer use. The authors explicitly prohibit using this software, its protocols, or its libraries for developing malicious tools, malware, unauthorized data exfiltration, or cyber attacks. The maintainers assume no liability for misuse.

---

## Author

Created and maintained by **[Nova-Stark](https://github.com/Nova-Stark)**.

---

## License

This project is licensed under the terms of the [MIT License](LICENSE).  
Copyright (c) 2026 Nova-Stark.

