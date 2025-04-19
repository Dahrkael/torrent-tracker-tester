package bittorrent

import (
	"bytes"
	"reflect"
	"testing"
)

// TestMarshalBencode tests the MarshalBencode function with various types
func TestMarshalBencode(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []byte
		wantErr  bool
	}{
		{
			name:     "Integer",
			input:    int64(123),
			expected: []byte("i123e"),
			wantErr:  false,
		},
		{
			name:     "String",
			input:    "hello",
			expected: []byte("5:hello"),
			wantErr:  false,
		},
		{
			name:     "Empty String",
			input:    "",
			expected: []byte("0:"),
			wantErr:  false,
		},
		{
			name:     "List",
			input:    []interface{}{int64(1), "two", int64(3)},
			expected: []byte("li1e3:twoi3ee"),
			wantErr:  false,
		},
		{
			name: "Dictionary",
			input: map[string]interface{}{
				"key1": int64(42),
				"key2": "value",
			},
			expected: []byte("d4:key1i42e4:key25:valuee"),
			wantErr:  false,
		},
		{
			name: "Struct with tags",
			input: struct {
				Name string `bencode:"name"`
				Size int64  `bencode:"length"`
			}{
				Name: "test",
				Size: 1024,
			},
			expected: []byte("d6:lengthi1024e4:name4:teste"),
			wantErr:  false,
		},
		{
			name:     "Unsupported type",
			input:    float64(3.14),
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalBencode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalBencode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !bytes.Equal(got, tt.expected) {
				t.Errorf("MarshalBencode() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// TestUnmarshalBencode tests the UnmarshalBencode function with various bencode inputs
func TestUnmarshalBencode(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		target   interface{}
		expected interface{}
		wantErr  bool
	}{
		{
			name:     "Integer",
			input:    []byte("i123e"),
			target:   new(int64),
			expected: int64(123),
			wantErr:  false,
		},
		{
			name:     "String",
			input:    []byte("5:hello"),
			target:   new(string),
			expected: "hello",
			wantErr:  false,
		},
		{
			name:     "List",
			input:    []byte("li1e3:twoi3ee"),
			target:   new([]interface{}),
			expected: []interface{}{int64(1), "two", int64(3)},
			wantErr:  false,
		},
		{
			name:   "Dictionary",
			input:  []byte("d4:key1i42e4:key25:valuee"),
			target: new(map[string]interface{}),
			expected: map[string]interface{}{
				"key1": int64(42),
				"key2": "value",
			},
			wantErr: false,
		},
		{
			name:  "Struct with tags",
			input: []byte("d6:lengthi1024e4:name4:teste"),
			target: &struct {
				Name string `bencode:"name"`
				Size int64  `bencode:"length"`
			}{},
			expected: struct {
				Name string `bencode:"name"`
				Size int64  `bencode:"length"`
			}{
				Name: "test",
				Size: 1024,
			},
			wantErr: false,
		},
		{
			name:     "Invalid integer",
			input:    []byte("ixxe"),
			target:   new(int64),
			expected: int64(0),
			wantErr:  true,
		},
		{
			name:     "Unterminated list",
			input:    []byte("li1e3:two"),
			target:   new([]interface{}),
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UnmarshalBencode(tt.input, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalBencode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				targetValue := reflect.ValueOf(tt.target).Elem().Interface()
				if !reflect.DeepEqual(targetValue, tt.expected) {
					t.Errorf("UnmarshalBencode() = %v, want %v", targetValue, tt.expected)
				}
			}
		})
	}
}

// TestRoundTrip tests that encoding and decoding a value results in the same value
func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{
			name:  "Integer",
			input: int64(456),
		},
		{
			name:  "String",
			input: "roundtrip",
		},
		{
			name:  "List",
			input: []interface{}{int64(1), "test", int64(999)},
		},
		{
			name: "Dictionary",
			input: map[string]interface{}{
				"a": int64(1),
				"b": "value",
			},
		},
		{
			name: "Struct",
			input: struct {
				Name string `bencode:"name"`
				Size int64  `bencode:"size"`
			}{
				Name: "file",
				Size: 2048,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the input
			encoded, err := MarshalBencode(tt.input)
			if err != nil {
				t.Errorf("MarshalBencode() error = %v", err)
				return
			}

			// Create a new target of the same type
			targetType := reflect.TypeOf(tt.input)
			target := reflect.New(targetType).Interface()

			// Unmarshal into the target
			err = UnmarshalBencode(encoded, target)
			if err != nil {
				t.Errorf("UnmarshalBencode() error = %v", err)
				return
			}

			// Compare the original input with the unmarshaled result
			result := reflect.ValueOf(target).Elem().Interface()
			if !reflect.DeepEqual(result, tt.input) {
				t.Errorf("Round trip failed: got %v, want %v", result, tt.input)
			}
		})
	}
}
