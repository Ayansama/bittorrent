package dht

import (
	"crypto/rand"
	"math/big"
	"sort"
	"sync"
)

const k = 8

type RoutingTable struct {
	mu    sync.Mutex
	own   [20]byte
	nodes []*Node
}

func NewRoutingTable() (*RoutingTable, error) {
	var id [20]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &RoutingTable{own: id}, nil
}

func (rt *RoutingTable) OwnID() [20]byte {
	return rt.own
}

func (rt *RoutingTable) Add(n *Node) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if n.ID == rt.own {
		return
	}

	for _, existing := range rt.nodes {
		if existing.ID == n.ID {
			return
		}
	}

	rt.nodes = append(rt.nodes, n)

	own := rt.own
	sort.Slice(rt.nodes, func(i, j int) bool {
		di := xorDist(rt.nodes[i].ID, own)
		dj := xorDist(rt.nodes[j].ID, own)
		return di.Cmp(dj) < 0
	})

	if len(rt.nodes) > 160*k {
		rt.nodes = rt.nodes[:160*k]
	}
}

func (rt *RoutingTable) Closest(target [20]byte, n int) []*Node {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	cp := make([]*Node, len(rt.nodes))
	copy(cp, rt.nodes)

	sort.Slice(cp, func(i, j int) bool {
		di := xorDist(cp[i].ID, target)
		dj := xorDist(cp[j].ID, target)
		return di.Cmp(dj) < 0
	})

	if n > len(cp) {
		n = len(cp)
	}
	return cp[:n]
}

func (rt *RoutingTable) Size() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.nodes)
}

func xorDist(a, b [20]byte) *big.Int {
	var x [20]byte
	for i := range x {
		x[i] = a[i] ^ b[i]
	}
	return new(big.Int).SetBytes(x[:])
}