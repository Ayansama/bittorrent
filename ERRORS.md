# Build Log — Errors & Solutions

> A running record of every significant error encountered building the BitTorrent client from scratch in Go, and exactly how each was fixed.

---

## Phase 1 — Foundations

---

### Error 1 — Handshake Never Sent

**File:** `peer/handshake.go`

**Symptom:**
Every peer connection returned `EOF` or `i/o timeout` immediately after connecting. The peer would accept the TCP connection and then drop it.

**Error message:**

```
Failed to read handshake: EOF
Failed to read handshake: read tcp ... i/o timeout
```

**Root cause:**
The handshake message was being built correctly but never actually written to the connection. The code jumped straight from building `hs` to reading the response — so we were waiting for the peer's handshake while the peer was waiting for ours. Classic deadlock.

```go
// ❌ Built the message but never sent it
hs := make([]byte, 0, 68)
hs = append(hs, pstrLen...)
// ...
resp := make([]byte, 68)  // jumped straight to reading
io.ReadFull(conn, resp)
```

**Fix:**

```go
// ✅ Added the missing Write call
if _, err := conn.Write(hs); err != nil {
    return nil, fmt.Errorf("failed to send handshake: %w", err)
}
```

---

### Error 2 — Typo in Protocol String Validation

**File:** `peer/handshake.go`

**Symptom:**
Every handshake response was rejected with "invalid protocol string" even from peers that were clearly running valid BitTorrent clients.

**Error message:**

```
invalid protocol string
```

**Root cause:**
A typo in the validation string — "BitTirrent" instead of "BitTorrent". Every valid response was being rejected. lol

```go
// ❌ Wrong — "BitTirrent" (typo)
if string(resp[1:20]) != "BitTirrent protocol" {
```

**Fix:**

```go
// ✅ Correct spelling
if string(resp[1:20]) != "BitTorrent protocol" {
```

---

### Error 3 — Tracker Returning Only 1 Peer

**File:** `main.go`

**Symptom:**
Ubuntu's tracker always returned exactly 1 peer regardless of how many times we called it. That 1 peer consistently failed to handshake.

**Error message:**

```
Got 1 peers
Trying 185.125.190.59:6893 ... failed
```

**Root cause:**
Ubuntu's tracker (`torrent.ubuntu.com`) is intentionally conservative — it only returns 1-5 peers per request and only serves peers for official Ubuntu torrents. It's not a general-purpose public tracker.

**Fix:**
Added public UDP trackers as fallback:

```go
trackers = append(trackers,
    "udp://tracker.opentrackr.org:1337/announce",
    "udp://open.stealth.si:80/announce",
    "udp://tracker.torrent.eu.org:451/announce",
)
```

---

## Phase 1 — Network Issues

---

### Error 4 — All UDP Trackers Timing Out (University/Corporate Network)

**Symptom:**
All UDP tracker connections timed out. HTTP trackers worked fine.

**Error message:**

```
connect response failed: read udp 172.27.1.106:56840->93.158.213.92:1337: i/o timeout
```

**Root cause:**
The network (`172.x.x.x` private IP range) was a university/corporate network with outbound UDP blocked at the firewall level. UDP packets never left the network.

**Diagnosis:**

```powershell
Test-NetConnection -ComputerName router.bittorrent.com -Port 6881
# TcpTestSucceeded: False
```

**Fix:**
Switched to a mobile hotspot. Mobile networks don't block UDP by default. did'nt knew it before

---

### Error 5 — All Handshakes Failing on Mobile Hotspot

**Symptom:**
Even on mobile hotspot with 100 peers, every handshake failed with timeout or connection refused.

**Error message:**

```
i/o timeout
connection refused
```

**Root cause:**
Indian mobile carriers (Jio, Airtel) perform deep packet inspection and block BitTorrent traffic at the carrier level. Port 6881 specifically was blocked.

**Diagnosis:**

```powershell
Test-NetConnection -ComputerName 5.182.32.144 -Port 6881
# PingSucceeded: True  ← can reach the IP
# TcpTestSucceeded: False  ← TCP blocked
```

**Fix:**
Used a VPN (Windscribe — free tier, P2P allowed). After connecting:

```
TcpTestSucceeded: True
```

---

### Error 6 — Peers Rejecting Plaintext Handshake (EOF / Forcibly Closed)

**Symptom:**
After VPN, TCP connections succeeded but peers immediately dropped the connection after receiving our handshake.

**Error message:**

```
Failed to read handshake: EOF
wsarecv: An existing connection was forcibly closed by the remote host
```

**Root cause:**
These peers require MSE (Message Stream Encryption). They reject plaintext BitTorrent handshakes — common in clients configured for maximum privacy or in swarms with encryption enforced.

**Fix:**
Implemented MSE encryption in `peer/mse.go`:

- Diffie-Hellman key exchange (768-bit prime)
- RC4 stream cipher with 1024-byte keystream discard
- `ConnectAndHandshake` tries MSE first, falls back to plaintext

```go
// Try MSE first
mseConn, err := PerformMSEHandshake(conn, infoHash)
if err == nil {
    result, err := DoHandshake(mseConn, infoHash, peerID)
    if err == nil {
        return mseConn, result, nil
    }
}
// Fall back to plaintext
```

## Phase 2 — Message Protocol

---

### Error 7 — Deadlock After All Goroutines Failed

**Symptom:**
When all peer handshakes failed, the program hung indefinitely instead of exiting cleanly.

**Error message:**

```
fatal error: all goroutines are asleep - deadlock!
goroutine 1 [chan receive]:
main.main()
```

**Root cause:**
Main was blocking on `r := <-found` waiting for a successful connection, but all goroutines had exited without sending to the channel. Nobody would ever send, so main blocked forever.

**Fix:**
Added a `failed` channel to count failures and exit when all goroutines have reported failure:

```go
failed := make(chan struct{}, len(allPeers))

// In goroutine:
failed <- struct{}{}

// In main:
failCount := 0
for {
    select {
    case r = <-found:
        goto connected
    case <-failed:
        failCount++
        if failCount >= limit {
            fmt.Println("All peers failed.")
            return
        }
    }
}
```

---

### Error 8 — Peer Choked Mid-Download

**Symptom:**
Download started but stopped partway through a piece when the peer decided to choke us.

**Error message:**

```
peer choked us mid-download
exit status 1
```

**Root cause:**
BitTorrent peers run a choking algorithm — they periodically re-evaluate which peers to serve. Getting choked mid-download is normal behavior that needs to be handled gracefully.

**Fix:**
Updated `DownloadPiece` to wait for unchoke and resume instead of returning an error:

```go
case peer.MsgChoke:
    choked = true
    peer.SendMessage(conn, peer.MsgInterested, nil)
    goto nextBlock

// At top of loop:
if choked {
    for {
        msg, _ := peer.ReadMessage(conn)
        if msg.ID == peer.MsgUnchoke {
            choked = false
            break
        }
    }
}
```

---

## Phase 3 — Piece Download

---

### Error 9 — meta.Hashes Undefined

**File:** `main.go`

**Symptom:**
Compile error — field name mismatch between `PieceManager` and `TorrentMeta`.

**Error message:**

```
meta.Hashes undefined (type *torrent.TorrentMeta has no field or method Hashes)
```

**Root cause:**
`TorrentMeta` uses the field name `Pieces` but `PieceManager` internally used `Hashes`. The names didn't match when accessed from `main.go`.

**Fix:**
Renamed `Hashes` to `Pieces` in `PieceManager` to match `TorrentMeta`:

```go
// ❌ Before
Hashes [][20]byte

// ✅ After
Pieces [][20]byte
```

---

## Phase 4 — Concurrency

---

### Error 10 — Workers Stopping Too Early

**Symptom:**
Download would reach 0.7% and stop. Workers were exiting before all pieces were downloaded.

**Output:**

```
0.7% (165/24868 pieces) — 0.26 MB/s
Stopped: 165/24868 pieces
```

**Root cause:**
Each worker goroutine was tied to a single peer. When a peer choked them or disconnected, the goroutine exited. With no mechanism to try new peers, the pool quickly emptied.

**Fix:**
Replaced static goroutine-per-peer with a worker pool backed by a peer channel. Each worker tries peers from the channel one by one — when one peer dies, it immediately picks up the next:

```go
peerCh := make(chan tracker.PeerAddr, len(allPeers))
for _, p := range allPeers {
    peerCh <- p
}
close(peerCh)

for i := 0; i < 30; i++ {
    go func() {
        for p := range peerCh {
            runWorker(p, ...)
        }
    }()
}
```

---

### Error 11 — Progress Output Garbled

**Symptom:**
The progress percentage and download debug logs were overwriting each other on the same terminal line, producing unreadable output.

**Output:**

```
  progress: 256 / 256 KB— 0.03 MB/s    choked — waiting...
  progress: 256 / 256 KB8 pieces)
```

**Root cause:**
Two different `fmt.Printf` calls with `\r` (carriage return) were fighting — the progress reporter in `main.go` and the block-level progress inside `DownloadPiece`.

**Fix:**
Removed all `fmt.Printf` progress lines from `DownloadPiece` — only the top-level progress reporter in `main.go` prints to the terminal.

---

## Phase 5 — Endgame

---

### Error 12 — Endgame Causing Memory Exhaustion

**Symptom:**
Laptop froze at 99% when endgame triggered. Go runtime ran out of memory.

**Error message:**

```
fatal error: runtime: cannot allocate memory
```

**Root cause:**
Endgame was spawning `remaining_pieces × active_peers` goroutines simultaneously. With 29 pieces × 19 peers = 551 goroutines, each allocating a 256 KB piece buffer, that's ~140 MB just for buffers plus goroutine overhead.

**Fix:**
Rewrote endgame to process one piece at a time with a maximum of 8 peers per piece:

```go
for _, idx := range remaining {
    // try up to 8 peers sequentially, not all at once
    for i := 0; i < 8 && i < len(conns); i++ {
        data, err := DownloadPiece(conns[i], idx, pl)
        if err == nil && Verify(data, hashes[idx]) == nil {
            fw.WritePiece(idx, data)
            pm.Complete(idx)
            break
        }
    }
}
```

---

### Error 13 — Endgame Not Triggering (No Active Connections)

**Symptom:**
Endgame code ran but had no connections to use. Last few pieces never downloaded.

**Output:**

```
Workers done: 3010/3020 pieces
Stopped: 3010/3020 pieces (99.7%)
```

**Root cause:**
Endgame was called after `wg.Wait()` — by then all worker goroutines had already exited and closed their connections. `activeConns` was empty.

**Fix:**
Moved endgame to make **fresh connections** for each remaining piece instead of relying on connections left over from the worker pool:

```go
func RunEndgame(allPeers []tracker.PeerAddr, ...) {
    for _, idx := range remaining {
        for i := 0; i < 20; i++ {
            // Dial a brand new connection for each attempt
            conn, err := net.DialTimeout("tcp", allPeers[i].AddrString(), 5*time.Second)
            // ... handshake, download, verify
        }
    }
}
```

---

## Phase 6 — DHT & Magnet Links

---

### Error 14 — IPv6 Address Formatting

**File:** `tracker/udp.go`, `pieces/endgame.go`

**Symptom:**
Connecting to IPv6 peers failed because the address string was malformed.

**Error message:**

```
dial tcp: address 2001:db8::1:6881: too many colons in address
```

**Root cause:**
IPv6 addresses already contain colons. Formatting them as `ip:port` produced ambiguous strings like `2001:db8::1:6881` where Go couldn't tell where the IP ended and the port began.

**Fix:**
Added `AddrString()` method to `PeerAddr` that wraps IPv6 addresses in brackets:

```go
func (p PeerAddr) AddrString() string {
    ip := net.ParseIP(p.IP)
    if ip != nil && ip.To4() == nil {
        return fmt.Sprintf("[%s]:%d", p.IP, p.Port)  // IPv6
    }
    return fmt.Sprintf("%s:%d", p.IP, p.Port)  // IPv4
}
```

---

### Error 15 — readFull Assignment Mismatch

**File:** `dht/metadata.go`

**Symptom:**
Compile error on the `readFull` call.

**Error message:**

```
assignment mismatch: 2 variables but readFull returns 1 value
```

**Root cause:**
`readFull` only returns `error`, but was called with two return variables using `_, err :=`.

```go
// ❌ Wrong
if _, err := readFull(conn, resp); err != nil {
```

**Fix:**

```go
// ✅ Correct — readFull returns only error
if err := readFull(conn, resp); err != nil {
```

---

### Error 16 — DHT Bootstrap Timing Out

**Symptom:**
DHT bootstrap consistently failed with timeout, but downloads still worked because tracker peers were available.

**Error message:**

```
DHT bootstrap failed: bootstrap failed: read failed: read udp [::]:59495: i/o timeout
```

**Root cause:**
VPN was blocking outbound UDP to `router.bittorrent.com:6881`. DHT uses UDP, which some VPN servers restrict.

**Impact:**
Non-fatal — the client fell back to tracker-discovered peers for metadata fetch. DHT is an enhancement, not a requirement, so the download completed successfully without it.

**Partial fix:**
The `FetchMagnetMetadata` function was designed to gracefully degrade — DHT failure is caught and logged, then the function continues with whatever peers the tracker returned:

```go
if err := d.Bootstrap(); err != nil {
    fmt.Printf("  DHT bootstrap failed: %v\n", err)
    // continues with knownPeers from trackers
}
```

---

## Summary

| #   | Phase       | Error                             | Fix                             |
| --- | ----------- | --------------------------------- | ------------------------------- |
| 1   | Handshake   | Never sent the handshake message  | Added `conn.Write(hs)`          |
| 2   | Handshake   | Typo "BitTirrent"                 | Fixed spelling                  |
| 3   | Tracker     | Ubuntu tracker returns 1 peer     | Added public UDP trackers       |
| 4   | Network     | UDP blocked on university network | Switched to mobile hotspot      |
| 5   | Network     | Carrier blocking BitTorrent ports | Used VPN                        |
| 6   | Handshake   | Peers rejecting plaintext         | Implemented MSE encryption      |
| 7   | Network     | ProtonVPN blocking P2P            | Switched to Windscribe          |
| 8   | Concurrency | Deadlock when all peers fail      | Added failure counter channel   |
| 9   | Download    | Peer choked mid-download          | Handle choke, wait, resume      |
| 10  | Compile     | Field name mismatch Hashes/Pieces | Renamed to Pieces               |
| 11  | Concurrency | Workers stopping too early        | Worker pool with peer channel   |
| 12  | Output      | Progress output garbled           | Removed redundant printf calls  |
| 13  | Endgame     | Memory exhaustion at 99%          | Process one piece at a time     |
| 14  | Endgame     | No connections available          | Fresh connections per piece     |
| 15  | Network     | IPv6 address format               | AddrString() with brackets      |
| 16  | Compile     | Node/DecodeNodes redeclared       | Kept only in krpc.go            |
| 17  | Compile     | readFull return value mismatch    | Removed extra variable          |
| 18  | DHT         | Bootstrap UDP timeout             | Graceful degradation to tracker |
