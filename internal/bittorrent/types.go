package bittorrent

// Peer represents the basic information of a peer
type Peer struct {
	IP   string
	Port int
}

// TrackerResponse represents the generic tracker response
type TrackerResponse struct {
	FailureReason string      `bencode:"failure reason"`
	Interval      int         `bencode:"interval"`
	Peers         interface{} `bencode:"peers"` // Can be a compact string or a list of peer dictionaries
}

// NonCompactPeer represents a peer in non-compact format (dictionary)
type NonCompactPeer struct {
	IP     string `bencode:"ip"`
	Port   int    `bencode:"port"`
	PeerID string `bencode:"peer_id"`
}

// Tracker is the interface that defines tracker behavior
type Tracker interface {
	Announce(infoHash string, stats TransferStats) ([]Peer, error)
}

// ClientConfig holds the client configuration
type ClientConfig struct {
	PeerID  string
	Address Peer
}

// TransferStats holds the transfer statistics for an announce request
type TransferStats struct {
	Uploaded   int64 // Total bytes uploaded so far
	Downloaded int64 // Total bytes downloaded so far
	Left       int64 // Bytes remaining to download
}
