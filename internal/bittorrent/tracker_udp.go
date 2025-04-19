package bittorrent

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"time"
)

// UDPTracker implements the Tracker interface for UDP trackers
type UDPTracker struct {
	trackerURL string
	config     ClientConfig
	conn       *net.UDPConn
	timeout    time.Duration
}

// NewUDPTracker creates a new UDP tracker with the provided configuration and tracker URL
func NewUDPTracker(trackerURL string, config ClientConfig, timeout time.Duration) (*UDPTracker, error) {
	// Parse the full URL to extract host and port
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing tracker URL: %v", err)
	}

	if u.Scheme != "udp" {
		return nil, fmt.Errorf("tracker URL must use udp scheme, found '%s'", u.Scheme)
	}

	// Resolve UDP address using only host and port
	addr, err := net.ResolveUDPAddr("udp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("error resolving UDP address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("error connecting to UDP tracker: %v", err)
	}

	return &UDPTracker{
		trackerURL: trackerURL,
		config:     config,
		conn:       conn,
		timeout:    timeout,
	}, nil
}

// Announce for UDPTracker sends a request to the UDP tracker
func (ut *UDPTracker) Announce(infoHash string, stats TransferStats) ([]Peer, error) {
	hashBytes, err := hex.DecodeString(infoHash)
	if err != nil || len(hashBytes) != 20 {
		return nil, fmt.Errorf("invalid info_hash: must be a 40-character hexadecimal hash")
	}

	connID, err := ut.connect()
	if err != nil {
		return nil, err
	}

	peers, err := ut.announce(connID, hashBytes, stats)
	if err != nil {
		return nil, err
	}
	defer ut.conn.Close() // Close the UDP connection upon completion
	return peers, nil
}

// connect sends the connection message to the UDP tracker
func (ut *UDPTracker) connect() (int64, error) {
	const protocolID = 0x41727101980 // BitTorrent UDP protocol ID
	transactionID := rand.Int31()

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint64(protocolID))
	binary.Write(buf, binary.BigEndian, uint32(0)) // Action: connect (0)
	binary.Write(buf, binary.BigEndian, uint32(transactionID))

	ut.conn.SetDeadline(time.Now().Add(ut.timeout))
	_, err := ut.conn.Write(buf.Bytes())
	if err != nil {
		return 0, fmt.Errorf("error sending connect: %v", err)
	}

	resp := make([]byte, 16)
	ut.conn.SetDeadline(time.Now().Add(ut.timeout))
	n, err := ut.conn.Read(resp)
	if err != nil || n < 16 {
		return 0, fmt.Errorf("error receiving connect response: %v (read %d bytes)", err, n)
	}

	action := binary.BigEndian.Uint32(resp[0:4])
	respTransactionID := binary.BigEndian.Uint32(resp[4:8])
	connID := int64(binary.BigEndian.Uint64(resp[8:16]))

	if action != 0 || respTransactionID != uint32(transactionID) {
		return 0, fmt.Errorf("invalid connect response")
	}

	return connID, nil
}

// announce sends the announce message to the UDP tracker
func (ut *UDPTracker) announce(connID int64, infoHash []byte, stats TransferStats) ([]Peer, error) {
	transactionID := rand.Int31()

	buf := new(bytes.Buffer)
	// connection_id: 64-bit ID received from the connect response
	binary.Write(buf, binary.BigEndian, connID)
	// action: 1 indicates an announce request
	binary.Write(buf, binary.BigEndian, int32(1)) // Action: announce (1)
	// transaction_id: Random 32-bit ID to match request and response
	binary.Write(buf, binary.BigEndian, transactionID)
	// info_hash: The 20-byte SHA-1 hash of the torrent's info dictionary
	buf.Write(infoHash)
	// peer_id: A unique 20-byte identifier for this client
	buf.WriteString(ut.config.PeerID)
	// downloaded: Total bytes downloaded so far (64-bit)
	binary.Write(buf, binary.BigEndian, stats.Downloaded)
	// left: Bytes remaining to download (64-bit, estimated total size)
	binary.Write(buf, binary.BigEndian, stats.Left)
	// uploaded: Total bytes uploaded so far (64-bit)
	binary.Write(buf, binary.BigEndian, stats.Uploaded)
	// event: 0 = none, 1 = completed, 2 = started, 3 = stopped (32-bit)
	binary.Write(buf, binary.BigEndian, int32(0))
	// ip: Optional IP address (0 = default, tracker uses sender IP)
	binary.Write(buf, binary.BigEndian, uint32(0))
	// key: Random 32-bit key for tracker to verify client (not used here)
	binary.Write(buf, binary.BigEndian, uint32(0))
	// num_want: Number of peers desired (-1 = default)
	binary.Write(buf, binary.BigEndian, int32(-1))
	// port: The port this client is listening on (16-bit)
	binary.Write(buf, binary.BigEndian, uint16(ut.config.Address.Port))

	// BEP 41 URLData extension (option-type 0x2)
	trackerURL, err := url.Parse(ut.trackerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid tracker URL: %v", err)
	}

	pathQuery := trackerURL.Path
	if trackerURL.RawQuery != "" {
		pathQuery += "?" + trackerURL.RawQuery
	}

	if pathQuery != "" {
		// option-type (1 byte) + length (1 byte) + data
		buf.WriteByte(0x2)                  // option-type: URLData
		buf.WriteByte(byte(len(pathQuery))) // length of PATH + QUERY
		buf.WriteString(pathQuery)          // PATH and QUERY components
	}

	ut.conn.SetDeadline(time.Now().Add(ut.timeout))
	_, err = ut.conn.Write(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("error sending announce: %v", err)
	}

	resp := make([]byte, 1024)
	ut.conn.SetDeadline(time.Now().Add(ut.timeout))
	n, err := ut.conn.Read(resp)
	if err != nil || n < 20 {
		return nil, fmt.Errorf("error receiving announce response: %v", err)
	}

	action := binary.BigEndian.Uint32(resp[0:4])
	respTransactionID := binary.BigEndian.Uint32(resp[4:8])
	if action != 1 || respTransactionID != uint32(transactionID) {
		return nil, fmt.Errorf("invalid announce response")
	}

	peersData := resp[20:n]
	return parseCompactPeers(string(peersData))
}
