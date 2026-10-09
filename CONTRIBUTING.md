# Contributing to mittodrop

Thank you for your interest in contributing to `mittodrop`! This project aims to provide a fast, robust, and zero-knowledge end-to-end encrypted file and directory transfer CLI.

To maintain code quality, security guarantees, and cross-platform reliability, please review this guide before submitting issues or pull requests.

---

## Code of Conduct & Ethical Boundaries

`mittodrop` is developed strictly for lawful and authorized data transfer. Contributors must adhere to ethical open-source standards. PRs introducing obfuscated code, telemetry without consent, backdoors, or features intended for malicious operations will be rejected immediately.

---

## Development Prerequisites

- **Go**: Version 1.24+ (or Go 1.27+)
- **Git**
- **Make** (optional, recommended for running common build targets)

### Getting Started

```bash
# 1. Fork and clone the repository
git clone https://github.com/Nova-Stark/mittodrop-cli.git
cd mittodrop-cli

# 2. Build the binary
make build

# 3. Verify tests pass locally
make test
```

The compiled binary will be placed at `bin/mittodrop` (or `bin/mittodrop.exe` on Windows).

---

## Developer Documentation & Architecture

For deep architectural and cryptographic breakdowns, see our dedicated developer documentation:
-  **[System Architecture Guide](docs/ARCHITECTURE.md)** — Connection flows, streaming TAR memory pipes, and subsystem interactions.
-  **[Cryptographic & Wire Specification](docs/SPECIFICATION.md)** — SPAKE2 key exchange, AES-256-GCM framing format, and security mitigations.

---

## Developer Architecture & Codebase Map

To help you navigate the codebase quickly, here is a roadmap of the primary packages:

| Package | Path | Responsibility |
| :--- | :--- | :--- |
| **CLI Entrypoint** | `cmd/mittodrop` | Main entrypoint, version injection, and execution delegator. |
| **App Workflows** | `cmd/app` | Command execution flows (`direct`, `shout`, `otin`, `relay serve`). |
| **CLI Parser** | `internal/cliparser` | Argument parsing, flag validation, non-destructive file checking. |
| **Transfer Engine** | `internal/transfer` | Streaming chunk pipeline, AEAD encryption/decryption, checksums. |
| **Archive Streaming** | `internal/transfer/archive.go` | On-the-fly streaming TAR reader/writer with zero temp disk footprint. |
| **Key Exchange** | `internal/pake` | Zero-knowledge PAKE password-authenticated key exchange. |
| **Direct P2P** | `internal/manual` | Direct TCP listeners with port-hunting fallback and dialers. |
| **LAN Discovery** | `internal/shout` | Local network discovery and announcement. |
| **Relay Server & Client** | `internal/relay` | Rendezvous WebSocket server and bi-directional stream pipes. |
| **UPnP Port Mapping** | `internal/portmap` | IGD UPnP port forwarding for direct WAN connections. |
| **Utilities** | `internal/utils` | Port availability detection, network interface inspection. |

---

## Developer Guidelines & Core Invariants

### 1. Security & Cryptography Invariants
- **Zero Downgrade**: Never bypass PAKE authentication or downgrade AEAD encryption primitives.
- **Never Log Secrets**: Codephrases, raw keys, tokens, or plaintext payload buffers must **never** be printed to stdout, stderr, or loggers.
- **Path Traversal Defense**: All file extraction logic must strictly enforce path confinement (`filepath.Clean` + jail prefix checks) to prevent Zip-Slip vulnerabilities.

### 2. Concurrency & Goroutine Hygiene
- Always accept and propagate `context.Context` across network listeners, dials, and stream copy loops.
- Every spawned goroutine must have a deterministic termination path (via context cancellation, channel closure, or connection EOF). No leaked background routines.

### 3. Cross-Platform Compatibility
- The application runs on Linux, macOS, and Windows.
- Always use `filepath.Join` and `filepath.Clean` instead of hardcoded forward (`/`) or backslashes (`\`).
- Isolate any platform-specific syscalls behind Go build tags (e.g., `//go:build windows`).

### 4. Port Conflict Prevention in Tests
- Never hardcode fixed port numbers in tests. Use dynamic port binding (`:0`) or probe functions so tests can execute concurrently without colliding on ports.

---

## Commit Message Convention

We strictly follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```text
<type>(<optional scope>): <description>

[optional body]

[optional footer(s)]
```

### Allowed Types:
- `feat`: A new user-facing or protocol feature
- `fix`: A bug fix
- `docs`: Documentation changes only
- `test`: Adding missing tests or correcting existing tests
- `refactor`: A code change that neither fixes a bug nor adds a feature
- `perf`: A code change that improves performance
- `chore`: Maintenance tasks, dependency updates, build tooling

*Examples:*
- `feat(transfer): add on-the-fly streaming tar recursive folder transfer`
- `fix(manual): add ephemeral port fallback when candidate ports are occupied`
- `test(app): add direct send directory integration test`

---

## Testing & Quality Assurance

Before submitting any code, ensure all quality checks pass locally:

```bash
# Run complete test suite sequentially (avoids port collisions)
make test

# Format code
make fmt

# Run static linter
make lint

# Verify and tidy module dependencies
make tidy
```

---

## Pull Request Guidelines

1. **Include Tests for New Logic**: Every bug fix or new feature **must** include corresponding unit or integration tests that prove the fix or feature works as intended.
2. **Minimal Surgical Changes**: Keep PRs focused. Do not combine unrelated refactors, formatting sweeps, or multiple features into a single PR.
3. **Keep Branches Rebased**: Ensure your branch is rebased on the latest `master` branch without merge conflicts.

### Contributor Checklist
Before opening your PR, verify:
- [ ] Code compiles cleanly across targets (`make build`).
- [ ] New logic is covered by unit or integration tests.
- [ ] All tests pass without flake (`make test`).
- [ ] Code is formatted with `go fmt` (`make fmt`).
- [ ] Linter checks pass (`make lint`).
- [ ] Commit messages follow the Conventional Commits specification.
- [ ] Documentation updated if CLI flags or user behaviors were added.
