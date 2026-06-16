package dht

import (
	"bittorrent/peer"
	"bittorrent/torrent"
	"bittorrent/tracker"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const (
	extHandshake = 0
	extMetadata  = 1
)

// FetchMagnetMetadata finds peers via DHT then fetches
// the torrent metadata from them using BEP 9.
func FetchMagnetMetadata(infoHash [20]byte, peerID [20]byte,
	knownPeers []tracker.PeerAddr) (*torrent.TorrentMeta, error) {

	// Step 1: Use DHT to find more peers
	fmt.Println("  Starting DHT bootstrap...")
	d, err := NewDHT()
	if err != nil {
		fmt.Printf("  DHT init failed: %v — using known peers only\n", err)
	} else {
		defer d.Close()
		if err := d.Bootstrap(); err != nil {
			fmt.Printf("  DHT bootstrap failed: %v\n", err)
		} else {
			dhtPeers, err := d.GetPeers(infoHash)
			if err == nil && len(dhtPeers) > 0 {
				fmt.Printf("  DHT found %d peers\n", len(dhtPeers))
				knownPeers = append(knownPeers, dhtPeers...)
			}
		}
	}

	if len(knownPeers) == 0 {
		return nil, fmt.Errorf("no peers available for metadata fetch")
	}

	// Step 2: Try each peer until we get metadata
	limit := len(knownPeers)
	if limit > 30 {
		limit = 30
	}

	for i := 0; i < limit; i++ {
		p := knownPeers[i]
		addr := p.AddrString()
		fmt.Printf("  Trying peer %s for metadata...\n", addr)

		meta, err := fetchMetadataFromPeer(addr, infoHash, peerID)
		if err != nil {
			fmt.Printf("  Failed: %v\n", err)
			continue
		}
		return meta, nil
	}

	return nil, fmt.Errorf("could not fetch metadata from any peer")
}

// fetchMetadataFromPeer connects to a single peer and
// fetches torrent metadata using the extension protocol.
func fetchMetadataFromPeer(addr string, infoHash, peerID [20]byte) (*torrent.TorrentMeta, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Send extension handshake — set reserved bit 20 to signal BEP 10 support
	// Byte 5 of reserved bytes, bit 4 = extension protocol
	reserved := make([]byte, 8)
	reserved[5] = 0x10

	// Build handshake with extension bit set
	hs := make([]byte, 0, 68)
	hs = append(hs, 19)
	hs = append(hs, []byte("BitTorrent protocol")...)
	hs = append(hs, reserved...)
	hs = append(hs, infoHash[:]...)
	hs = append(hs, peerID[:]...)

	if _, err := conn.Write(hs); err != nil {
		return nil, fmt.Errorf("handshake send failed: %w", err)
	}

	// Read peer handshake
	resp := make([]byte, 68)
	if err := readFull(conn, resp); err != nil {
		return nil, fmt.Errorf("handshake read failed: %w", err)
	}

	// Check peer supports extensions (reserved byte 5, bit 4)
	if resp[25]&0x10 == 0 {
		return nil, fmt.Errorf("peer does not support extension protocol")
	}

	// Send extension handshake (message ID 20, ext ID 0)
	extHs := torrent.Encode(map[string]interface{}{
		"m": map[string]interface{}{
			"ut_metadata": 1,
		},
	})
	if err := sendExtMessage(conn, extHandshake, 0, extHs); err != nil {
		return nil, fmt.Errorf("ext handshake failed: %w", err)
	}

	// Read messages until we get extension handshake response
	var metadataSize int
	var peerExtID int

	for {
		msg, err := peer.ReadMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("read failed: %w", err)
		}

		// Message ID 20 = extension message
		if msg.ID != 20 {
			continue
		}
		if len(msg.Payload) < 1 {
			continue
		}

		extID := int(msg.Payload[0])
		payload := msg.Payload[1:]

		if extID == extHandshake {
			// Parse extension handshake to get ut_metadata ID and size
			decoded, _, err := torrent.Decode(payload, 0)
			if err != nil {
				return nil, fmt.Errorf("ext hs decode failed: %w", err)
			}
			d := decoded.(map[string]interface{})

			// Get peer's ut_metadata extension ID
			if m, ok := d["m"].(map[string]interface{}); ok {
				if id, ok := m["ut_metadata"].(int64); ok {
					peerExtID = int(id)
				}
			}
			// Get metadata size
			if size, ok := d["metadata_size"].(int64); ok {
				metadataSize = int(size)
			}

			if peerExtID == 0 {
				return nil, fmt.Errorf("peer does not support ut_metadata")
			}
			if metadataSize == 0 {
				return nil, fmt.Errorf("peer did not send metadata_size")
			}

			// Request metadata pieces (each piece is 16KB)
			numPieces := (metadataSize + 16383) / 16384
			fmt.Printf("  metadata: %d bytes, %d pieces\n", metadataSize, numPieces)

			for i := 0; i < numPieces; i++ {
				req := torrent.Encode(map[string]interface{}{
					"msg_type": int64(0), // request
					"piece":    int64(i),
				})
				if err := sendExtMessage(conn, extMetadata, peerExtID, req); err != nil {
					return nil, fmt.Errorf("metadata request failed: %w", err)
				}
			}
			break
		}
	}

	// Collect metadata pieces
	metadataData := make([]byte, metadataSize)
	received := make(map[int]bool)
	numPieces := (metadataSize + 16383) / 16384

	for len(received) < numPieces {
		msg, err := peer.ReadMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("read metadata piece failed: %w", err)
		}

		if msg.ID != 20 || len(msg.Payload) < 1 {
			continue
		}
		if int(msg.Payload[0]) != extMetadata {
			continue
		}

		payload := msg.Payload[1:]

		// Find where bencoded dict ends and raw data begins
		decoded, end, err := torrent.Decode(payload, 0)
		if err != nil {
			continue
		}

		d := decoded.(map[string]interface{})
		msgType, _ := d["msg_type"].(int64)
		pieceIdx, _ := d["piece"].(int64)

		if msgType != 1 { // 1 = data
			continue
		}

		// Raw metadata comes after the bencoded dict
		pieceData := payload[end:]
		offset := int(pieceIdx) * 16384
		copy(metadataData[offset:], pieceData)
		received[int(pieceIdx)] = true

		fmt.Printf("  got metadata piece %d/%d\n", len(received), numPieces)
	}

	// Verify metadata SHA1 matches info_hash
	actual := sha1.Sum(metadataData)
	if actual != infoHash {
		return nil, fmt.Errorf("metadata hash mismatch")
	}

	fmt.Println("  metadata verified ✓")

	// Parse the metadata as a torrent info dict
	return parseInfoDict(metadataData, infoHash)
}

func sendExtMessage(conn net.Conn, extID, peerExtID int, payload []byte) error {
	// Extension message: [msg_id=20][ext_id][payload]
	msg := make([]byte, 2+len(payload))
	msg[0] = 20 // extension message ID
	if extID == extHandshake {
		msg[1] = 0
	} else {
		msg[1] = byte(peerExtID)
	}
	copy(msg[2:], payload)

	// Length prefix
	buf := make([]byte, 4+len(msg))
	binary.BigEndian.PutUint32(buf[0:], uint32(len(msg)))
	copy(buf[4:], msg)

	_, err := conn.Write(buf)
	return err
}

func readFull(conn net.Conn, buf []byte) error {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func parseInfoDict(data []byte, infoHash [20]byte) (*torrent.TorrentMeta, error) {
	decoded, _, err := torrent.Decode(data, 0)
	if err != nil {
		return nil, fmt.Errorf("info dict decode failed: %w", err)
	}

	info, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid info dict")
	}

	rawPieces, ok := info["pieces"].([]byte)
	if !ok || len(rawPieces)%20 != 0 {
		return nil, fmt.Errorf("invalid pieces field")
	}

	pieces := make([][20]byte, len(rawPieces)/20)
	for i := range pieces {
		copy(pieces[i][:], rawPieces[i*20:(i+1)*20])
	}

	meta := &torrent.TorrentMeta{
		InfoHash:    infoHash,
		PieceLength: info["piece length"].(int64),
		Pieces:      pieces,
		Name:        string(info["name"].([]byte)),
	}

	if length, ok := info["length"].(int64); ok {
		meta.Length = length
	} else if files, ok := info["files"].([]interface{}); ok {
		for _, f := range files {
			fmap := f.(map[string]interface{})
			fi := torrent.FileInfo{Length: fmap["length"].(int64)}
			for _, p := range fmap["path"].([]interface{}) {
				fi.Path = append(fi.Path, string(p.([]byte)))
			}
			meta.Files = append(meta.Files, fi)
			meta.Length += fi.Length
		}
	}

	return meta, nil
}