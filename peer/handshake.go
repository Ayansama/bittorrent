package peer

import (
	"fmt"
	"io"
	"net"
	"time"
)

var(
	pstrLen = []byte{19}
	pstr = []byte("BitTorrent protocol")
	reserved = make([]byte ,8)
)

type HandshakeResult struct{
	RemotePeerID [20]byte
}

func DoHandshake(conn net.Conn, infoHash, peerID [20]byte) (*HandshakeResult, error) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Build 68-byte handshake message
	hs := make([]byte, 0, 68)
	hs = append(hs, pstrLen...)
	hs = append(hs, pstr...)
	hs = append(hs, reserved...)
	hs = append(hs, infoHash[:]...)
	hs = append(hs, peerID[:]...)

	// ✅ Bug 2 fix — actually send it
	if _, err := conn.Write(hs); err != nil {
		return nil, fmt.Errorf("failed to send handshake: %w", err)
	}

	resp := make([]byte, 68)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("Failed to read handshake: %w", err)
	}

	if resp[0] != 19 {
		return nil, fmt.Errorf("invalid pstrlen: %d", resp[0])
	}

	// ✅ Bug 1 fix — correct spelling
	if string(resp[1:20]) != "BitTorrent protocol" {
		return nil, fmt.Errorf("invalid protocol string")
	}

	var gotHash [20]byte
	copy(gotHash[:], resp[28:48])
	if gotHash != infoHash {
		return nil, fmt.Errorf("info_hash mismatch")
	}

	var remoteID [20]byte
	copy(remoteID[:], resp[48:68])

	return &HandshakeResult{RemotePeerID: remoteID}, nil
}

func ConnectAndHandshake(addr string, infoHash, peerID [20]byte) (net.Conn, *HandshakeResult, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	// Try MSE encrypted handshake first
	mseConn, err := PerformMSEHandshake(conn, infoHash)
	if err == nil {
		result, err := DoHandshake(mseConn, infoHash, peerID)
		if err == nil {
			return mseConn, result, nil
		}
	}

	// MSE failed — reconnect and try plaintext
	conn.Close()
	conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to reconnect to %s: %w", addr, err)
	}

	result, err := DoHandshake(conn, infoHash, peerID)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("handshake failed with %s: %w", addr, err)
	}
	return conn, result, nil
}