package dht

import (
	"crypto/rand"
	"fmt"
)

var bootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"router.utorrent.com:6881",
	"dht.aelitis.com:6881",
}

func GenerateNodeID() ([20]byte, error) {
	var id [20]byte
	_, err := rand.Read(id[:])
	return id, err
}

func Bootstrap(krpc *KRPC, nodeID [20]byte) ([]*Node, error) {
	var allNodes []*Node

	for _, addr := range bootstrapNodes {
		fmt.Printf("  bootstrapping from %s...\n", addr)
		resp, err := krpc.SendQuery(addr, map[string]interface{}{
			"t": "aa",
			"y": "q",
			"q": "find_node",
			"a": map[string]interface{}{
				"id":     string(nodeID[:]),
				"target": string(nodeID[:]),
			},
		})
		if err != nil {
			fmt.Printf("  failed: %v\n", err)
			continue
		}

		r, ok := resp["r"].(map[string]interface{})
		if !ok {
			continue
		}

		nodesRaw, ok := r["nodes"].([]byte)
		if !ok {
			continue
		}

		nodes := DecodeNodes(nodesRaw)
		fmt.Printf("  got %d nodes\n", len(nodes))
		allNodes = append(allNodes, nodes...)
	}

	return allNodes, nil
}