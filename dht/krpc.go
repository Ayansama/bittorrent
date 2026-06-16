package dht

import (
	"bittorrent/torrent"
	"fmt"
	"net"
	"time"
)

type Node struct {
	ID   [20]byte
	IP   string
	Port int
}

func (n *Node) AddrString() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

type KRPC struct {
	conn *net.UDPConn
}

func NewKRPC() (*KRPC, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return nil, fmt.Errorf("failed to open UDP socket: %w", err)
	}
	return &KRPC{conn: conn}, nil
}

func (k *KRPC) Close() {
	k.conn.Close()
}

func (k *KRPC) SendQuery(addr string, msg map[string]interface{}) (map[string]interface{}, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	encoded := torrent.Encode(msg)
	k.conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = k.conn.WriteToUDP(encoded, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("send failed: %w", err)
	}

	buf := make([]byte, 4096)
	n, _, err := k.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	decoded, _, err := torrent.Decode(buf[:n], 0)
	if err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	resp, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response type")
	}

	// Check for error response
	if errVal, ok := resp["y"]; ok {
		if string(errVal.([]byte)) == "e" {
			return nil, fmt.Errorf("KRPC error response")
		}
	}

	return resp, nil
}

func DecodeNodes(raw []byte) []*Node {
	var nodes []*Node
	// Each node is 26 bytes: 20 ID + 4 IP + 2 port
	for i := 0; i+26 <= len(raw); i += 26 {
		var id [20]byte
		copy(id[:], raw[i:i+20])
		ip := net.IP(raw[i+20 : i+24]).String()
		port := int(raw[i+24])<<8 | int(raw[i+25])
		if port == 0 {
			continue
		}
		nodes = append(nodes, &Node{
			ID:   id,
			IP:   ip,
			Port: port,
		})
	}
	return nodes
}

func DecodePeers(values []interface{}) []string {
	var peers []string
	for _, v := range values {
		raw := v.([]byte)
		if len(raw) < 6 {
			continue
		}
		ip   := net.IP(raw[0:4]).String()
		port := int(raw[4])<<8 | int(raw[5])
		if port > 0 {
			peers = append(peers, fmt.Sprintf("%s:%d", ip, port))
		}
	}
	return peers
}