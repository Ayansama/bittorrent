# BitTorrent Client in Go

> ⚠️ **Work in Progress** — core download pipeline is complete and verified. Several features are still being built.

A BitTorrent client built from scratch in Go, with no external BitTorrent libraries. Every component — from bencode parsing to DHT peer discovery — is implemented by hand for deep understanding of how the protocol works.

---

## Verified Downloads

The client has successfully downloaded and SHA256-verified the following:

| File                            | Size    | Source        | SHA256           |
| ------------------------------- | ------- | ------------- | ---------------- |
| ubuntu-26.04-desktop-amd64.iso  | 6.07 GB | .torrent file | `487f87fa...` ✅ |
| debian-13.5.0-amd64-netinst.iso | 0.74 GB | .torrent file | `95838884...` ✅ |
| debian-13.5.0-amd64-netinst.iso | 0.74 GB | magnet link   | `95838884...` ✅ |

All downloads verified against official checksums published by Ubuntu and Debian.

---

## Features

- **Bencode** — full encode/decode implementation
- **Torrent file parsing** — extracts info_hash, piece hashes, file list
- **Magnet link support** — parses magnet URIs including hex and base32 info hashes
- **Tracker communication** — HTTP and UDP tracker protocols (BEP 15)
- **Peer handshake** — standard BitTorrent handshake + MSE encryption fallback (Diffie-Hellman + RC4)
- **Full message protocol** — all 9 message types (choke, unchoke, interested, have, bitfield, request, piece, cancel, keepalive)
- **Concurrent downloading** — goroutine-per-peer worker pool with configurable concurrency
- **SHA1 piece verification** — every piece verified before writing to disk
- **Resume support** — interrupted downloads resume from where they stopped
- **Endgame mode** — last few pieces requested from multiple peers simultaneously
- **DHT** — Kademlia-based distributed hash table for trackerless peer discovery (BEP 5)
- **BEP 9 metadata exchange** — fetches torrent metadata directly from peers using magnet links

---

## Architecture

```
bittorrent/
│
├── main.go                   Entry point — CLI, worker pool, progress reporting
│
├── torrent/
│   ├── bencode.go            Bencode encoder/decoder
│   ├── metainfo.go           .torrent file parser, TorrentMeta struct
│   └── magnet.go             Magnet link parser (hex + base32 info hash)
│
├── tracker/
│   └── udp.go                HTTP + UDP tracker announce (BEP 15)
│
├── peer/
│   ├── peer_id.go            20-byte client identifier generation
│   ├── handshake.go          68-byte BitTorrent handshake + MSE fallback
│   ├── mse.go                Message Stream Encryption (Diffie-Hellman + RC4)
│   ├── messages.go           Message framing, encode/decode, all message types
│   └── connection.go         Unchoke flow, peer state management
│
├── pieces/
│   ├── downloader.go         Block requests, choke handling, piece assembly
│   ├── piece_manager.go      Concurrent piece assignment, completion tracking
│   └── endgame.go            Last-piece broadcast to all peers
│
├── storage/
│   ├── file_writer.go        Verified pieces written to correct byte offsets
│   └── resume.go             Persist/load completed piece set to disk
│
└── dht/
    ├── krpc.go               UDP KRPC messaging, Node struct, compact peer decode
    ├── routing_table.go      Kademlia k-bucket routing table, XOR distance
    ├── dht.go                Bootstrap + iterative get_peers lookup
    └── metadata.go           BEP 9 metadata fetch from peers
```

### Data Flow

```
magnet link / .torrent file
        ↓
   torrent/metainfo.go or torrent/magnet.go
        ↓
   tracker/udp.go  +  dht/dht.go          ← peer discovery
        ↓
   peer/handshake.go  +  peer/mse.go      ← connection + encryption
        ↓
   peer/messages.go                        ← protocol messaging
        ↓
   pieces/piece_manager.go                 ← work assignment
        ↓
   pieces/downloader.go                    ← block download + SHA1 verify
        ↓
   storage/file_writer.go                  ← verified data → disk
```

---

## Usage

```bash
# Clone and build
git clone https://github.com/Ayansama/bittorrent
cd bittorrent
go mod init bittorrent
go build ./...

# Run interactively (prompts for input)
go run main.go

# Pass a .torrent file directly
go run main.go ubuntu.torrent

# Pass a magnet link directly
go run main.go "magnet:?xt=urn:btih:..."
```

When prompted, paste either a `.torrent` filename or a full magnet link. The client will contact trackers, connect to peers, and begin downloading with live progress output:

```
Name:      debian-13.5.0-amd64-netinst.iso
InfoHash:  58846860f0a766f8a42b0bb214d8c713fdf1b167
Pieces:    3020 x 256 KB
Size:      0.74 GB

Found 92 unique peers
Downloading: debian-13.5.0-amd64-netinst.iso
Starting workers...
  98.8% (2985/3020 pieces) — 2.14 MB/s
  endgame — 5 pieces left
  endgame: piece 3017 ✓
Download complete! 0.74 GB in 11m38s
```

---

## How It Works

### Piece Download Pipeline

BitTorrent splits files into fixed-size pieces (typically 256 KB). Each piece has a known SHA1 hash stored in the `.torrent` file. The client:

1. Gets a list of peers from trackers or DHT
2. Connects to multiple peers simultaneously (goroutine per peer)
3. Requests 16 KB blocks from each peer
4. Assembles blocks into complete pieces
5. SHA1-verifies each piece before writing to disk
6. Discards and re-requests any piece that fails verification

### MSE Encryption

Many ISPs throttle BitTorrent traffic by detecting the plaintext handshake (`\x13BitTorrent protocol`). MSE (Message Stream Encryption) wraps the connection in RC4 encryption derived from a Diffie-Hellman shared secret, making traffic look like random bytes to DPI systems.

### DHT (Distributed Hash Table)

DHT eliminates the need for a central tracker. Nodes form a Kademlia network where each node stores information about peers near its own ID in XOR distance space. To find peers for a torrent:

1. Bootstrap from `router.bittorrent.com:6881`
2. Iteratively query the closest known nodes with `get_peers`
3. Each response returns either closer nodes or actual peer addresses
4. Feed discovered peers into the download engine

### BEP 9 Metadata Exchange

Magnet links contain only the info_hash — no piece hashes. BEP 9 lets the client request the full torrent metadata from a peer using the extension protocol:

1. Signal BEP 10 extension support in the handshake reserved bytes
2. Exchange extension handshakes to negotiate `ut_metadata` IDs
3. Request metadata in 16 KB pieces
4. Verify the assembled metadata SHA1 matches the info_hash
5. Parse piece hashes and begin downloading

---

## Implemented BEPs

| BEP    | Title                                      | Status      |
| ------ | ------------------------------------------ | ----------- |
| BEP 3  | BitTorrent Protocol                        | ✅ Complete |
| BEP 5  | DHT Protocol                               | ✅ Complete |
| BEP 9  | Extension for Peers to Send Metadata Files | ✅ Complete |
| BEP 10 | Extension Protocol                         | ✅ Complete |
| BEP 15 | UDP Tracker Protocol                       | ✅ Complete |
| BEP 23 | Tracker Returns Compact Peer Lists         | ✅ Complete |
| BEP 29 | uTP — Micro Transport Protocol             | 🔲 Planned  |
| BEP 11 | Peer Exchange (PEX)                        | 🔲 Planned  |
| BEP 14 | Local Service Discovery                    | 🔲 Planned  |

---

## Future Plans

- [ ] **Seeding/uploading** — accept incoming connections, upload pieces, implement tit-for-tat choking algorithm
- [ ] **Rarest-first piece selection** — prioritize pieces fewest peers have for better swarm health
- [ ] **Request pipelining** — keep 5-10 block requests in flight per peer for higher throughput
- [ ] **Multi-file torrent support** — map piece byte ranges across multiple output files
- [ ] **Selective download** — choose specific files from a multi-file torrent
- [ ] **PEX (Peer Exchange)** — peers share known peer lists with each other (BEP 11)
- [ ] **uTP transport** — BitTorrent over UDP for better congestion control (BEP 29)
- [ ] **TUI dashboard** — live terminal UI with per-peer speeds and piece map
- [ ] **REST API** — JSON control interface for remote management
- [ ] **.torrent creator** — generate .torrent files from local folders

---

## Requirements

- Go 1.21 or later
- No external BitTorrent libraries — standard library only
- VPN recommended on networks that throttle BitTorrent traffic

---

## Notes

This project was built for learning purposes — to understand BitTorrent internals by implementing every component from scratch. It is not intended to replace production clients like qBittorrent or Transmission.

All test downloads used publicly available, legally distributed Linux ISO files.

---

_Built from scratch in Go — every byte understood._
