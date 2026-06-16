package dht

import (
	"bittorrent/tracker"
	"fmt"
	"net"
	"time"
)

const bootstrapNode = "router.bittorrent.com:6881"

type DHT struct {
	krpc *KRPC
	rt   *RoutingTable
}

func NewDHT() (*DHT, error) {
	krpc, err := NewKRPC()
	if err != nil {
		return nil, err
	}

	rt, err := NewRoutingTable()
	if err != nil {
		krpc.Close()
		return nil, err
	}

	return &DHT{krpc: krpc, rt: rt}, nil
}

func (d *DHT) Close() {
	d.krpc.Close()
}

func (d *DHT) Bootstrap() error {
	nodeID := d.rt.OwnID()

	msg := map[string]interface{}{
		"t": "aa",
		"y": "q",
		"q": "find_node",
		"a": map[string]interface{}{
			"id":     string(nodeID[:]),
			"target": string(nodeID[:]),
		},
	}

	resp, err := d.krpc.SendQuery(bootstrapNode, msg)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	r, ok := resp["r"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid bootstrap response")
	}

	nodesRaw, ok := r["nodes"].([]byte)
	if !ok {
		return fmt.Errorf("no nodes in bootstrap response")
	}

	nodes := DecodeNodes(nodesRaw)
	for _, n := range nodes {
		d.rt.Add(n)
	}

	fmt.Printf("  bootstrap: got %d nodes, routing table: %d\n",
		len(nodes), d.rt.Size())
	return nil
}

func (d *DHT) GetPeers(infoHash [20]byte) ([]tracker.PeerAddr, error) {
	nodeID  := d.rt.OwnID()
	queried := make(map[string]bool)
	var found []tracker.PeerAddr

	candidates := d.rt.Closest(infoHash, 8)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("routing table empty — bootstrap first")
	}

	fmt.Printf("  DHT lookup: %d initial candidates\n", len(candidates))

	for round := 0; round < 10 && len(candidates) > 0; round++ {
		node := candidates[0]
		candidates = candidates[1:]

		addr := node.AddrString()
		if queried[addr] {
			continue
		}
		queried[addr] = true

		msg := map[string]interface{}{
			"t": "bb",
			"y": "q",
			"q": "get_peers",
			"a": map[string]interface{}{
				"id":        string(nodeID[:]),
				"info_hash": string(infoHash[:]),
			},
		}

		resp, err := d.krpc.SendQuery(addr, msg)
		if err != nil {
			continue
		}

		r, ok := resp["r"].(map[string]interface{})
		if !ok {
			continue
		}

		// Actual peer addresses
		if values, ok := r["values"].([]interface{}); ok {
			peers := DecodePeers(values)
			for _, p := range peers {
				host, portStr, err := net.SplitHostPort(p)
				if err != nil {
					continue
				}
				var port uint16
				fmt.Sscanf(portStr, "%d", &port)
				found = append(found, tracker.PeerAddr{
					IP:   host,
					Port: port,
				})
			}
			fmt.Printf("  DHT round %d: %d peers found so far\n", round, len(found))
		}

		// Closer nodes to query
		if nodesRaw, ok := r["nodes"].([]byte); ok {
			closer := DecodeNodes(nodesRaw)
			for _, n := range closer {
				if !queried[n.AddrString()] {
					d.rt.Add(n)
					candidates = append(candidates, n)
				}
			}
		}

		if len(found) >= 30 {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	fmt.Printf("  DHT lookup complete: %d peers\n", len(found))
	return found, nil
}