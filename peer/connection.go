package peer

import (
	"fmt"
	"net"
	"time"
)

type PeerConn struct {
	conn     net.Conn
	Addr     string
	Bitfield []byte
	AmChoked bool
}

func NewPeerConn(conn net.Conn, addr string,
	infoHash, peerID [20]byte) (*PeerConn, error) {

	pc := &PeerConn{
		conn:     conn,
		Addr:     addr,
		AmChoked: true,
	}
	return pc, nil
}

func (pc *PeerConn) Conn() net.Conn { return pc.conn }

func (pc *PeerConn) WaitUnchoke() error {
	pc.conn.SetDeadline(time.Now().Add(30 * time.Second))

	for {
		msg, err := ReadMessage(pc.conn)
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		switch msg.ID {
		case MsgBitfield:
			pc.Bitfield = msg.Payload
			fmt.Printf("  [%s] has %d pieces in bitfield\n",
				pc.Addr, countBits(pc.Bitfield))

			// Tell peer we want data
			if err := SendMessage(pc.conn, MsgInterested, nil); err != nil {
				return fmt.Errorf("send interested failed: %w", err)
			}
			fmt.Printf("  [%s] sent interested\n", pc.Addr)

		case MsgUnchoke:
			pc.AmChoked = false
			fmt.Printf("  [%s] unchoked! ready to request pieces\n", pc.Addr)
			return nil

		case MsgChoke:
			pc.AmChoked = true
			fmt.Printf("  [%s] choked\n", pc.Addr)

		case MsgHave:
			// Peer just downloaded a piece — update bitfield
			if len(msg.Payload) == 4 {
				idx := int(msg.Payload[0])<<24 | int(msg.Payload[1])<<16 |
					int(msg.Payload[2])<<8 | int(msg.Payload[3])
				fmt.Printf("  [%s] has piece %d\n", pc.Addr, idx)
			}

		default:
			fmt.Printf("  [%s] got message: %s\n", pc.Addr, MsgName(msg.ID))
		}
	}
}

// countBits counts how many pieces the peer has
func countBits(bitfield []byte) int {
	count := 0
	for _, b := range bitfield {
		for i := 0; i < 8; i++ {
			if (b>>uint(7-i))&1 == 1 {
				count++
			}
		}
	}
	return count
}