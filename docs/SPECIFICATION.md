# mittodrop Cryptographic & Wire Protocol Specification

This document defines the cryptographic primitives, zero-knowledge key agreement, wire framing format, and security threat mitigations implemented by `mittodrop`.

> **Navigation:**
> - [System Architecture](./ARCHITECTURE.md)
> - [Contributing Guide](../CONTRIBUTING.md)
> - [Security Policy](../SECURITY.md)

---

## 1. Cryptographic Primitives

`mittodrop` uses modern, audited cryptographic algorithms across all CLI and browser interactions:

| Component | Primitive | Purpose |
| :--- | :--- | :--- |
| **Key Agreement** | **SPAKE2** (SIEC Elliptic Curve) | Zero-Knowledge Password-Authenticated Key Exchange (`github.com/schollz/pake/v3`) |
| **Key Derivation** | **PBKDF2-HMAC-SHA256** | Key stretching with unique per-session salt (`internal/relay`) |
| **Payload Encryption** | **AES-256-GCM** (AEAD) | Hardware-accelerated authenticated stream encryption with 96-bit nonces |
| **Stream Integrity** | **SHA-256** | End-to-end checksum verification computed ahead-of-time and validated on receipt |
| **CLI Compression** | **Zstandard (zstd)** | High-throughput streaming compression with adaptive ratio thresholding |
| **Browser Compression** | **Gzip (`CompressionStream`)** | Native in-browser compression for LinkShare web uploads |

---

## 2. Zero-Knowledge Key Agreement (PAKE)

Key derivation uses SPAKE2 over the **SIEC** elliptic curve:

```
  Initiator (Sender)                                Responder (Receiver)
Shared Codephrase: "ocean-star-summit"             Shared Codephrase: "ocean-star-summit"
        |                                                   |
        |---- 1. Initiator Public Point (Bytes) ----------->|
        |                                                   |
        |<--- 2. Responder Public Point (Bytes) ------------|
        |                                                   |
[ Derive 256-bit Key ]                             [ Derive 256-bit Key ]
```

### Handshake Properties:
1. **Zero-Knowledge**: Neither peer transmits the codephrase or password hash over the wire. Only public elliptic curve points modified by the passphrase scalar are exchanged.
2. **Relay Blindness**: Relay servers only see opaque binary curve coordinates. The relay operator cannot decrypt payloads, cannot derive session keys, and cannot perform offline dictionary attacks.
3. **Active Attack Resistance**: An active attacker attempting to guess passwords can make at most **one** online guess per handshake attempt.
4. **Forward Secrecy**: Handshake points are generated ephemerally per connection. Compromise of long-term secrets does not compromise past transfer sessions.

---

## 3. Wire Framing Protocol (`internal/transport`)

Once a session key is established, all binary data is encapsulated in authenticated transport frames.

### 3.1 Frame Header Layout (19 Bytes)

Every frame begins with a fixed 19-byte plaintext header:

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Magic 0     |   Magic 1     |   MsgType     |               |
|    (0x4D)     |    (0x44)     |    (1 Byte)   |               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+               +
|                       Payload Length (uint32)                 |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                                                               |
+                       AES-GCM Nonce (12 Bytes)                +
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                 Encrypted Payload (Variable Length)           |
|                               ...                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                    GCM Auth Tag (16 Bytes)                    |
|                               ...                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

### 3.2 Header Field Descriptions:
- **Magic Bytes** (`2 Bytes`): `0x4D 0x44` (ASCII `"MD"`). Identifies valid `mittodrop` transport traffic and rejects alien protocol connections immediately.
- **MsgType** (`1 Byte`):
  - `0x01` (`MsgFileMeta`): File or directory metadata.
  - `0x02` (`MsgChunk`): Payload data chunk.
  - `0x03` (`MsgFileDone`): Sender has completed streaming all chunks (carries 32-byte SHA-256 stream trailer checksum).
  - `0x04` (`MsgFileAck`): Receiver ready signal or final SHA-256 verification ACK.
  - `0x05` (`MsgAbort`): Immediate cancellation or protocol violation.
- **Payload Length** (`4 Bytes`): Big-endian `uint32` specifying the ciphertext length. Enforces a strict **32 MB ceiling** to prevent memory exhaustion attacks.
- **Nonce** (`12 Bytes`): Standard GCM 96-bit initialization vector generated from a monotonic sequence counter combined with session salt.
- **Auth Tag** (`16 Bytes`): Authenticated tag appended to ciphertext and validated by AES-GCM before payload decryption.

### 3.3 Relay Server Framing (`internal/relay`)
For rendezvous relay coordination, frames use an 8-byte framing prefix:
`[4 Bytes Magic: "croc"] [4 Bytes Length (uint32 little-endian)] [Payload]`
Payloads passing through the relay server are end-to-end encrypted; the relay never processes decrypted payloads.

---

## 4. Adaptive Compression Strategy

`mittodrop` employs dual compression strategies depending on the client environment:

1. **CLI Engine (Zstandard)**:
   - Chunk payloads are compressed using Zstandard (`github.com/klauspost/compress/zstd`).
   - **Threshold Check**: If `len(compressed) >= 0.95 * len(raw)`, the chunk is marked uncompressed (`is_compressed: false`) and sent as raw bytes. This eliminates CPU overhead on uncompressible files (e.g. `.mp4`, `.zip`, `.gz`).
2. **Web Browser (Gzip via `CompressionStream`)**:
   - In LinkShare mode, modern browsers run native JavaScript `CompressionStream('gzip')`.
   - Small files (< 1 KB) or pre-compressed extensions are skipped to optimize browser performance.

---

## 5. Security & Threat Mitigation Guarantees

### 5.1 Replay & Message Reordering Attacks
- `Framer` tracks monotonic atomic sequence counters (`sendSeq`, `recvSeq`).
- Every frame utilizes a unique, never-repeating 12-byte GCM nonce.
- Out-of-order, duplicated, or replayed frames trigger immediate AEAD authentication tag failure and connection termination.

### 5.2 Directory Traversal & Zip-Slip Defense
- During recursive directory extraction, attackers could craft malicious archive paths (e.g., `../../../../etc/shadow`).
- `mittodrop` runs path sanitization on every single archive entry:
  - Cleans relative paths with `filepath.Clean`.
  - Rejects entries with absolute paths or volume names.
  - Validates that the normalized entry path starts with the cleaned destination root directory path + separator (`filepath.Separator`).
  - Aborts immediately if an entry attempts to escape the destination sandbox.

### 5.3 Truncation & Partial Delivery Attacks
- Network transfers may be interrupted or prematurely severed by attackers.
- In single-file transfers, files are staged in hidden files (`.<filename>.<nonce>.mittodrop`).
- Only when the entire stream is received and the recomputed SHA-256 matches `meta.Checksum` is the staging file atomically renamed to its destination.
- In directory streaming, if the connection terminates before `MsgFileDone` or if checksum verification fails, the extraction directory is flagged as incomplete and cleaned.

### 5.4 DoS & Memory Exhaustion Mitigations
- Maximum frame payload is strictly capped at 32 MB (`MaxFramePayload`).
- Relay server frame size is capped at 64 MB (`MaxMessageSize`).
- Memory buffers for chunking use fixed-size object recycling pools (`sync.Pool`) to eliminate memory spikes and garbage collection thrashing.
