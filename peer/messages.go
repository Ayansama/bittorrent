package peer

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

type MsgID uint8

const (
	MsgChoke         MsgID = 0
	MsgUnchoke       MsgID = 1
	MsgInterested    MsgID = 2
	MsgNotInterested MsgID = 3
	MsgHave          MsgID = 4
	MsgBitfield      MsgID = 5
	MsgRequest       MsgID = 6
	MsgPiece         MsgID = 7
	MsgCancel        MsgID = 8
)

type Message struct {
	ID      MsgID
	Payload []byte
}

func ReadMessage(conn net.Conn) (*Message, error) {
	// Read 4-byte length prefix
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read length: %w", err)
	}

	// Keepalive — length of 0, no message ID
	if length == 0 {
		return &Message{}, nil
	}

	// Read exactly `length` bytes
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return &Message{
		ID:      MsgID(buf[0]),
		Payload: buf[1:],
	}, nil
}

func SendMessage(conn net.Conn, id MsgID, payload []byte) error {
	// 4-byte length + 1-byte ID + payload
	length := uint32(1 + len(payload))
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:], length)
	buf[4] = byte(id)
	copy(buf[5:], payload)
	_, err := conn.Write(buf)
	return err
}

func MsgName(id MsgID) string {
	names := map[MsgID]string{
		MsgChoke: "choke", MsgUnchoke: "unchoke",
		MsgInterested: "interested", MsgNotInterested: "not_interested",
		MsgHave: "have", MsgBitfield: "bitfield",
		MsgRequest: "request", MsgPiece: "piece", MsgCancel: "cancel",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", id)
}