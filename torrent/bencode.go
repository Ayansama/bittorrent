package torrent

import (
	"fmt"
	"sort"
	"strconv"
)

func indexOf(data []byte, b byte, start int) int {
	for i := start; i < len(data); i++ {
		if data[i] == b {
			return i
		}
	}
	return -1
}

func Decode(data []byte, pos int) (interface{}, int, error) {
	if pos >= len(data) {
		return nil, pos, fmt.Errorf("unexpected end of data")
	}

	switch {
	case data[pos] == 'i':
		end := indexOf(data, 'e', pos+1)
		if end == -1 {
			return nil, pos, fmt.Errorf("unterminated integer")
		}
		n, err := strconv.ParseInt(string(data[pos+1:end]), 10, 64)
		if err != nil {
			return nil, pos, err
		}
		return n, end + 1, nil

	case data[pos] == 'l':
		pos++
		var list []interface{}
		for pos < len(data) && data[pos] != 'e' {
			val, next, err := Decode(data, pos)
			if err != nil {
				return nil, pos, err
			}
			list = append(list, val)
			pos = next
		}
		return list, pos + 1, nil

	case data[pos] == 'd':
		pos++
		dict := make(map[string]interface{})
		for pos < len(data) && data[pos] != 'e' {
			key, next, err := Decode(data, pos)
			if err != nil {
				return nil, pos, err
			}
			val, next2, err := Decode(data, next)
			if err != nil {
				return nil, pos, err
			}
			dict[string(key.([]byte))] = val
			pos = next2
		}
		return dict, pos + 1, nil

	default: // byte string: "4:spam"
		colon := indexOf(data, ':', pos)
		if colon == -1 {
			return nil, pos, fmt.Errorf("invalid string encoding")
		}
		n, err := strconv.Atoi(string(data[pos:colon]))
		if err != nil {
			return nil, pos, err
		}
		start := colon + 1
		if start+n > len(data) {
			return nil, pos, fmt.Errorf("string length out of bounds")
		}
		return data[start : start+n], start + n, nil
	}
}

func Encode(v interface{}) []byte {
	switch val := v.(type) {
	case int:
		return []byte(fmt.Sprintf("i%de", val))
	case int64:
		return []byte(fmt.Sprintf("i%de", val))
	case string:
		return []byte(fmt.Sprintf("%d:%s", len(val), val))
	case []byte:
		return append([]byte(fmt.Sprintf("%d:", len(val))), val...)
	case []interface{}:
		buf := []byte("l")
		for _, item := range val {
			buf = append(buf, Encode(item)...)
		}
		return append(buf, 'e')
	case map[string]interface{}:
		// Keys must be sorted
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte("d")
		for _, k := range keys {
			buf = append(buf, Encode(k)...)
			buf = append(buf, Encode(val[k])...)
		}
		return append(buf, 'e')
	}
	return nil
}