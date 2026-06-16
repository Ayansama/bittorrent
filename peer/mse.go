package peer

import (
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
)

// Fixed 768-bit prime P and generator G=2 from the MSE spec
var mseP, _ = new(big.Int).SetString(
	"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1"+
		"29024E088A67CC74020BBEA63B139B22514A08798E3404DD"+
		"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245"+
		"E485B576625E7EC6F44C42E9A63A3620FFFFFFFFFFFFFFFF",
	16,
)
var mseG = big.NewInt(2)

type MSEConn struct {
	conn   net.Conn
	encRC4 *rc4.Cipher
	decRC4 *rc4.Cipher
}

func (m *MSEConn) Write(p []byte) (int, error) {
	encrypted := make([]byte, len(p))
	m.encRC4.XORKeyStream(encrypted, p)
	return m.conn.Write(encrypted)
}

func (m *MSEConn) Read(p []byte) (int, error) {
	n, err := m.conn.Read(p)
	if n > 0 {
		m.decRC4.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (m *MSEConn) Close() error                { return m.conn.Close() }
func (m *MSEConn) LocalAddr() net.Addr         { return m.conn.LocalAddr() }
func (m *MSEConn) RemoteAddr() net.Addr        { return m.conn.RemoteAddr() }
func (m *MSEConn) SetDeadline(t time.Time) error      { return m.conn.SetDeadline(t) }
func (m *MSEConn) SetReadDeadline(t time.Time) error  { return m.conn.SetReadDeadline(t) }
func (m *MSEConn) SetWriteDeadline(t time.Time) error { return m.conn.SetWriteDeadline(t) }

// PerformMSEHandshake wraps a raw TCP connection with MSE encryption.
// Returns a net.Conn whose reads/writes are transparently encrypted.
func PerformMSEHandshake(conn net.Conn, infoHash [20]byte) (net.Conn, error) {
	// Step 1: Generate our private key (random 160-bit number)
	privKey, err := rand.Int(rand.Reader, mseP)
	if err != nil {
		return nil, fmt.Errorf("keygen failed: %w", err)
	}

	// Step 2: Compute public key = G^privKey mod P
	pubKey := new(big.Int).Exp(mseG, privKey, mseP)

	// Step 3: Send our public key (padded to 96 bytes)
	pubKeyBytes := make([]byte, 96)
	pk := pubKey.Bytes()
	copy(pubKeyBytes[96-len(pk):], pk)
	if _, err := conn.Write(pubKeyBytes); err != nil {
		return nil, fmt.Errorf("send pubkey failed: %w", err)
	}

	// Step 4: Read peer's public key (96 bytes)
	peerPubKeyBytes := make([]byte, 96)
	if _, err := io.ReadFull(conn, peerPubKeyBytes); err != nil {
		return nil, fmt.Errorf("read peer pubkey failed: %w", err)
	}
	peerPubKey := new(big.Int).SetBytes(peerPubKeyBytes)

	// Step 5: Compute shared secret = peerPubKey^privKey mod P
	sharedSecret := new(big.Int).Exp(peerPubKey, privKey, mseP)
	secretBytes := make([]byte, 96)
	sb := sharedSecret.Bytes()
	copy(secretBytes[96-len(sb):], sb)

	// Step 6: Derive RC4 keys from shared secret + info_hash
	encKey := deriveKey("keyA", secretBytes, infoHash)
	decKey := deriveKey("keyB", secretBytes, infoHash)

	encCipher, err := rc4.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("enc cipher failed: %w", err)
	}
	decCipher, err := rc4.NewCipher(decKey)
	if err != nil {
		return nil, fmt.Errorf("dec cipher failed: %w", err)
	}

	// Step 7: Discard first 1024 bytes of keystream (RC4 weakness mitigation)
	discard := make([]byte, 1024)
	encCipher.XORKeyStream(discard, discard)
	decCipher.XORKeyStream(discard, discard)

	mseConn := &MSEConn{
		conn:   conn,
		encRC4: encCipher,
		decRC4: decCipher,
	}

	// Step 8: Send SKEY verification hash + crypto negotiation
	// HASH('req1', secret)
	req1 := sha1.Sum(append([]byte("req1"), secretBytes...))
	if _, err := conn.Write(req1[:]); err != nil {
		return nil, fmt.Errorf("send req1 failed: %w", err)
	}

	// HASH('req2', SKEY) XOR HASH('req3', secret)  — tells peer which torrent
	req2 := sha1.Sum(append([]byte("req2"), infoHash[:]...))
	req3 := sha1.Sum(append([]byte("req3"), secretBytes...))
	skey := make([]byte, 20)
	for i := range skey {
		skey[i] = req2[i] ^ req3[i]
	}
	if _, err := conn.Write(skey); err != nil {
		return nil, fmt.Errorf("send skey failed: %w", err)
	}

	// Send crypto negotiation (encrypted): VC + crypto_provide + pad + len
	vc            := make([]byte, 8)   // verification constant, all zeros
	cryptoProvide := []byte{0, 0, 0, 2} // 0x02 = RC4
	padLen        := []byte{0, 0}       // no padding
	ia            := []byte{0, 0}       // initial payload length = 0

	negotiation := append(vc, cryptoProvide...)
	negotiation  = append(negotiation, padLen...)
	negotiation  = append(negotiation, ia...)
	mseConn.Write(negotiation)

	// Step 9: Read peer's negotiation response (encrypted)
	vcResp := make([]byte, 8)
	if _, err := io.ReadFull(mseConn, vcResp); err != nil {
		return nil, fmt.Errorf("read VC failed: %w", err)
	}

	cryptoSelect := make([]byte, 4)
	if _, err := io.ReadFull(mseConn, cryptoSelect); err != nil {
		return nil, fmt.Errorf("read crypto_select failed: %w", err)
	}

	// Read and discard peer's padding
	padLenResp := make([]byte, 2)
	if _, err := io.ReadFull(mseConn, padLenResp); err != nil {
		return nil, fmt.Errorf("read pad len failed: %w", err)
	}
	pLen := binary.BigEndian.Uint16(padLenResp)
	if pLen > 0 {
		pad := make([]byte, pLen)
		io.ReadFull(mseConn, pad)
	}

	selected := binary.BigEndian.Uint32(cryptoSelect)
	if selected&2 == 0 {
		return nil, fmt.Errorf("peer rejected RC4 encryption")
	}

	return mseConn, nil
}

func deriveKey(prefix string, secret []byte, infoHash [20]byte) []byte {
	h := sha1.New()
	h.Write([]byte(prefix))
	h.Write(secret)
	h.Write(infoHash[:])
	return h.Sum(nil)
}