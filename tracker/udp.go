package tracker

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"time"
)

const magicConnID uint64 = 0x41727101980

type PeerAddr struct {
	IP   string
	Port uint16
}

func (p PeerAddr) AddrString() string {
	ip := net.ParseIP(p.IP)
	if ip != nil && ip.To4() == nil {
		// IPv6 — wrap in brackets
		return fmt.Sprintf("[%s]:%d", p.IP, p.Port)
	}
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

func UDPGetPeers(trackerAddr string, infoHash, peerID [20]byte, left int64) ([]PeerAddr, error) {
	// Strip udp:// and /announce
	addr, err := parseUDPAddr(trackerAddr)
	if err != nil {
		return nil, err
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve failed: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Step 1: Connect request
	txID := rand.Uint32()
	connReq := make([]byte, 16)
	binary.BigEndian.PutUint64(connReq[0:], magicConnID)
	binary.BigEndian.PutUint32(connReq[8:], 0) // action = connect
	binary.BigEndian.PutUint32(connReq[12:], txID)

	if _, err := conn.Write(connReq); err != nil {
		return nil, fmt.Errorf("connect send failed: %w", err)
	}

	// Step 2: Connect response
	connResp := make([]byte, 16)
	n, err := conn.Read(connResp)
	if err != nil || n < 16 {
		return nil, fmt.Errorf("connect response failed: %w", err)
	}
	connID := binary.BigEndian.Uint64(connResp[8:])

	// Step 3: Announce request
	txID2 := rand.Uint32()
	ann := make([]byte, 98)
	binary.BigEndian.PutUint64(ann[0:], connID)
	binary.BigEndian.PutUint32(ann[8:], 1) // action = announce
	binary.BigEndian.PutUint32(ann[12:], txID2)
	copy(ann[16:], infoHash[:])
	copy(ann[36:], peerID[:])
	binary.BigEndian.PutUint64(ann[56:], 0)              // downloaded
	binary.BigEndian.PutUint64(ann[64:], uint64(left))   // left
	binary.BigEndian.PutUint64(ann[72:], 0)              // uploaded
	binary.BigEndian.PutUint32(ann[80:], 0)              // event = none
	binary.BigEndian.PutUint32(ann[84:], 0)              // ip = default
	binary.BigEndian.PutUint32(ann[88:], rand.Uint32())  // key
	binary.BigEndian.PutUint32(ann[92:], 50)             // numwant
	binary.BigEndian.PutUint16(ann[96:], 6881)           // port

	if _, err := conn.Write(ann); err != nil {
		return nil, fmt.Errorf("announce send failed: %w", err)
	}

	// Step 4: Announce response
	buf := make([]byte, 2048)
	n, err = conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("announce response failed: %w", err)
	}
	if n < 20 {
		return nil, fmt.Errorf("announce response too short: %d", n)
	}

	// Parse peers from response (starts at byte 20)
	var peers []PeerAddr
	for i := 20; i+6 <= n; i += 6 {
		ip := net.IP(buf[i : i+4]).String()
		port := binary.BigEndian.Uint16(buf[i+4 : i+6])
		if port == 0 {
			continue
		}
		peers = append(peers, PeerAddr{IP: ip, Port: port})
	}
	return peers, nil
}

func parseUDPAddr(trackerURL string) (string, error) {
	// "udp://tracker.opentrackr.org:1337/announce" -> "tracker.opentrackr.org:1337"
	s := trackerURL
	if len(s) > 6 && s[:6] == "udp://" {
		s = s[6:]
	}
	for i, c := range s {
		if c == '/' {
			return s[:i], nil
		}
	}
	return s, nil
}