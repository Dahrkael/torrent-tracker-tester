package bittorrent

import (
	"reflect"
	"testing"
)

// TestparseCompactPeers tests the parseCompactPeers function with various inputs
func TestparseCompactPeers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Peer
		wantErr  bool
	}{
		{
			name:  "Single peer",
			input: string([]byte{127, 0, 0, 1, 26, 145}), // 127.0.0.1:6801
			expected: []Peer{
				{IP: "127.0.0.1", Port: 6801},
			},
			wantErr: false,
		},
		{
			name:  "Multiple peers",
			input: string([]byte{127, 0, 0, 1, 26, 145, 192, 168, 1, 1, 26, 146}), // 127.0.0.1:6801, 192.168.1.1:6802
			expected: []Peer{
				{IP: "127.0.0.1", Port: 6801},
				{IP: "192.168.1.1", Port: 6802},
			},
			wantErr: false,
		},
		{
			name:     "Empty input",
			input:    "",
			expected: []Peer{},
			wantErr:  false,
		},
		{
			name:     "Invalid length",
			input:    string([]byte{127, 0, 0, 1, 26}), // 5 bytes, not multiple of 6
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCompactPeers(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCompactPeers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseCompactPeers() = %v, want %v", got, tt.expected)
			}
		})
	}
}
