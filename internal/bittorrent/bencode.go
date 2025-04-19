package bittorrent

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// MarshalBencode encodes a value into bencode format
// Supported types: int64, string, []interface{}, map[string]interface{}, and structs with bencode tags
func MarshalBencode(value interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := encodeValue(&buf, reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeValue recursively encodes a value into bencode format
func encodeValue(buf *bytes.Buffer, v reflect.Value) error {
	// Handle nil values
	if !v.IsValid() {
		return fmt.Errorf("cannot encode nil value")
	}

	switch v.Kind() {
	case reflect.Int64:
		// Integers: "i<value>e"
		buf.WriteByte('i')
		buf.WriteString(strconv.FormatInt(v.Int(), 10))
		buf.WriteByte('e')

	case reflect.String:
		// Strings: "<length>:<value>"
		s := v.String()
		buf.WriteString(strconv.Itoa(len(s)))
		buf.WriteByte(':')
		buf.WriteString(s)

	case reflect.Slice, reflect.Array:
		// Lists: "l<elements>e"
		buf.WriteByte('l')
		for i := 0; i < v.Len(); i++ {
			if err := encodeValue(buf, v.Index(i)); err != nil {
				return err
			}
		}
		buf.WriteByte('e')

	case reflect.Map:
		// Dictionaries: "d<keys and values>e", keys sorted lexicographically
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("map keys must be strings")
		}
		buf.WriteByte('d')
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return bytes.Compare([]byte(keys[i].String()), []byte(keys[j].String())) < 0
		})
		for _, key := range keys {
			// Encode the key as a string
			buf.WriteString(strconv.Itoa(len(key.String())))
			buf.WriteByte(':')
			buf.WriteString(key.String())
			// Encode the value
			if err := encodeValue(buf, v.MapIndex(key)); err != nil {
				return err
			}
		}
		buf.WriteByte('e')

	case reflect.Struct:
		// Structs: encoded as dictionaries with field names from bencode tags
		buf.WriteByte('d')
		t := v.Type()
		fields := make([]structField, 0, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // Skip unexported fields
				continue
			}
			tag := f.Tag.Get("bencode")
			if tag == "" || tag == "-" {
				continue
			}
			// Split tag to handle "name,omitempty"
			tagParts := strings.Split(tag, ",")
			key := tagParts[0]
			omitempty := len(tagParts) > 1 && tagParts[1] == "omitempty"
			fieldValue := v.Field(i)
			// Skip if omitempty is set and the field is empty
			if omitempty && isEmpty(fieldValue) {
				continue
			}
			fields = append(fields, structField{name: key, value: fieldValue})
		}
		// Sort fields lexicographically by tag name
		// Sort fields lexicographically by raw bytes
		sort.Slice(fields, func(i, j int) bool {
			return bytes.Compare([]byte(fields[i].name), []byte(fields[j].name)) < 0
		})
		for _, f := range fields {
			// Encode the field name as a string
			buf.WriteString(strconv.Itoa(len(f.name)))
			buf.WriteByte(':')
			buf.WriteString(f.name)
			// Encode the field value
			if err := encodeValue(buf, f.value); err != nil {
				return err
			}
		}
		buf.WriteByte('e')

	case reflect.Interface:
		// Unwrap interface values
		return encodeValue(buf, v.Elem())

	default:
		return fmt.Errorf("unsupported type for bencode: %v", v.Kind())
	}
	return nil
}

// isEmpty determines if a value is considered "empty" for omitempty
func isEmpty(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int64:
		return v.Int() == 0
	case reflect.Slice, reflect.Array:
		return v.Len() == 0
	case reflect.Map:
		return v.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// structField holds a field name and value for sorting during encoding
type structField struct {
	name  string
	value reflect.Value
}

// UnmarshalBencode decodes a bencode string into a Go value
// The value must be a pointer to a supported type: int64, string, []interface{}, map[string]interface{}, or struct with bencode tags
func UnmarshalBencode(data []byte, value interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("empty bencode data")
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("value must be a non-nil pointer")
	}
	decoded, pos, err := decodeValue(data, 0)
	if err != nil {
		return err
	}
	if pos != len(data) {
		return fmt.Errorf("extra data after decoding: %d bytes remaining", len(data)-pos)
	}
	return assignValue(rv.Elem(), reflect.ValueOf(decoded))
}

// decodeValue recursively decodes a bencode string starting at the given position
func decodeValue(data []byte, pos int) (interface{}, int, error) {
	if pos >= len(data) {
		return nil, 0, fmt.Errorf("unexpected end of bencode data")
	}

	switch data[pos] {
	case 'i': // Integer
		end := pos + 1
		for end < len(data) && data[end] != 'e' {
			end++
		}
		if end >= len(data) {
			return nil, 0, fmt.Errorf("unterminated integer at position %d", pos)
		}
		numStr := string(data[pos+1 : end])
		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid integer at position %d: %v", pos, err)
		}
		return num, end + 1, nil

	case 'l': // List
		list := []interface{}{}
		pos++
		for pos < len(data) && data[pos] != 'e' {
			item, nextPos, err := decodeValue(data, pos)
			if err != nil {
				return nil, 0, err
			}
			list = append(list, item)
			pos = nextPos
		}
		if pos >= len(data) {
			return nil, 0, fmt.Errorf("unterminated list at position %d", pos)
		}
		return list, pos + 1, nil

	case 'd': // Dictionary
		dict := make(map[string]interface{})
		pos++
		for pos < len(data) && data[pos] != 'e' {
			key, nextPos, err := decodeString(data, pos)
			if err != nil {
				return nil, 0, fmt.Errorf("invalid dictionary key at position %d: %v", pos, err)
			}
			pos = nextPos
			value, nextPos, err := decodeValue(data, pos)
			if err != nil {
				return nil, 0, err
			}
			dict[key] = value
			pos = nextPos
		}
		if pos >= len(data) {
			return nil, 0, fmt.Errorf("unterminated dictionary at position %d", pos)
		}
		return dict, pos + 1, nil

	default: // String
		str, nextPos, err := decodeString(data, pos)
		if err != nil {
			return nil, 0, err
		}
		return str, nextPos, nil
	}
}

// decodeString decodes a bencode string (e.g., "5:hello") starting at the given position
func decodeString(data []byte, pos int) (string, int, error) {
	colon := pos
	for colon < len(data) && data[colon] != ':' {
		colon++
	}
	if colon >= len(data) {
		return "", 0, fmt.Errorf("invalid string at position %d: no colon found", pos)
	}
	lengthStr := string(data[pos:colon])
	length, err := strconv.Atoi(lengthStr)
	if err != nil || length < 0 {
		return "", 0, fmt.Errorf("invalid string length at position %d: %v", pos, err)
	}
	start := colon + 1
	end := start + length
	if end > len(data) {
		return "", 0, fmt.Errorf("string length exceeds data at position %d", pos)
	}
	return string(data[start:end]), end, nil
}

// assignValue assigns a decoded value to a target reflect.Value, handling structs with tags
func assignValue(target, source reflect.Value) error {
	if !source.IsValid() {
		return nil // Skip nil sources
	}

	// If source is an interface, unwrap it to get the underlying value
	if source.Kind() == reflect.Interface {
		source = source.Elem()
		if !source.IsValid() {
			return nil // Skip if unwrapped value is nil
		}
	}

	switch target.Kind() {
	case reflect.Int:
		fallthrough
	case reflect.Int64:
		if source.Kind() == reflect.Int64 {
			target.SetInt(source.Int())
		} else {
			return fmt.Errorf("cannot assign %v to int64", source.Kind())
		}

	case reflect.String:
		if source.Kind() == reflect.String {
			target.SetString(source.String())
		} else {
			return fmt.Errorf("cannot assign %v to string", source.Kind())
		}

	case reflect.Slice:
		if source.Kind() == reflect.Slice {
			target.Set(reflect.MakeSlice(target.Type(), source.Len(), source.Len()))
			for i := 0; i < source.Len(); i++ {
				if err := assignValue(target.Index(i), source.Index(i)); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("cannot assign %v to slice", source.Kind())
		}

	case reflect.Map:
		if source.Kind() == reflect.Map {
			target.Set(reflect.MakeMap(target.Type()))
			for _, key := range source.MapKeys() {
				value := source.MapIndex(key)
				targetKey := reflect.New(target.Type().Key()).Elem()
				targetValue := reflect.New(target.Type().Elem()).Elem()
				if err := assignValue(targetKey, key); err != nil {
					return err
				}
				if err := assignValue(targetValue, value); err != nil {
					return err
				}
				target.SetMapIndex(targetKey, targetValue)
			}
		} else {
			return fmt.Errorf("cannot assign %v to map", source.Kind())
		}

	case reflect.Struct:
		if source.Kind() == reflect.Map && source.Type().Key().Kind() == reflect.String {
			t := target.Type()
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if tag := f.Tag.Get("bencode"); tag != "" && tag != "-" {
					if val := source.MapIndex(reflect.ValueOf(tag)); val.IsValid() {
						// Ensure the value is assigned to the correct field type
						if err := assignValue(target.Field(i), val); err != nil {
							return fmt.Errorf("field '%s': %v", tag, err)
						}
					}
				}
			}
		} else {
			return fmt.Errorf("cannot assign %v to struct", source.Kind())
		}

	case reflect.Interface:
		target.Set(source)

	default:
		return fmt.Errorf("unsupported target type: %v", target.Kind())
	}
	return nil
}
