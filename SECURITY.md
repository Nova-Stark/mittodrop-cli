# Security Policy

`mittodrop` takes the security of its end-to-end encryption protocols, data transfer integrity, and user privacy seriously. This document outlines how vulnerabilities should be disclosed, how security issues are handled, and acceptable use guidelines.

---

## Supported Versions

Security updates and critical patches are actively applied to the latest version on the `master` branch and current release:

| Version | Supported | Notes |
| ------- | :-------: | ----- |
| **Latest (`master`)** |  Yes | Actively supported with security patches |
| **Older releases** |  No | Please update to the latest release for fixes |

---

## Acceptable Use Policy

`mittodrop` is designed and provided strictly for authorized, lawful data exchange and legitimate administrative or personal file sharing.

- **Prohibited Use**: This software, its libraries, protocols, or binaries must **not** be used for the development, distribution, or operation of malware, ransomware, spyware, command-and-control (C2) mechanisms, unauthorized exfiltration tooling, or any cyber-attack campaigns.
- **Limitation of Liability**: The authors and contributors assume no responsibility or liability for unauthorized or illegal use of this software.

---

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

If you discover a security vulnerability (such as flaws in cryptographic key derivation, authentication bypass, path traversal/Zip-Slip vulnerabilities, or denial-of-service vectors), please report it responsibly:

### Preferred Method: GitHub Private Vulnerability Reporting
1. Navigate to the [mittodrop-cli Security Advisory page](https://github.com/Nova-Stark/mittodrop-cli/security/advisories).
2. Click **"Report a vulnerability"** to submit your findings privately.

### Alternative Method: Direct Contact
If private reporting on GitHub is unavailable, you may reach out directly to the maintainer via GitHub profile contact: **[@Nova-Stark](https://github.com/Nova-Stark)**.

---

## What to Include in a Report

To help us investigate and resolve the issue quickly, please provide:
- A clear description of the vulnerability and its potential impact.
- Step-by-step instructions or a minimal proof of concept (PoC) to reproduce the issue.
- Operating system and Go version tested.
- Suggested remediations or patches, if available.

---

## Response Process

1. **Acknowledgment**: We aim to acknowledge receipt of your vulnerability report within 48 to 72 hours.
2. **Assessment**: The report will be triaged and verified in an isolated environment.
3. **Resolution**: Once validated, a fix will be developed in a private branch or security advisory draft.
4. **Coordinated Disclosure**: A patched release will be published along with appropriate security advisory credits to the finder (unless you prefer anonymity).
