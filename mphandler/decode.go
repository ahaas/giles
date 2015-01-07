package mphandler

import (
	"fmt"
)

func isstring(b byte) bool {
	return (b >= 0xa0 && b <= 0xbf)
}

func ismap(b byte) bool {
	return (b >= 0x80 && b <= 0x8f)
}

func isarray(b byte) bool {
	return (b >= 0x90 && b <= 0x9f)
}

// decodes msgpack string from byteslice
// returns deocded string and number of consumed bytes
func getstring(input *[]byte, offset int) (string, int) {
	length := int((*input)[offset] & 0x1f)
	if length == 0 {
		return "", 1
	}
	return string((*input)[offset+1 : offset+1+length]), length + 1
}

// return uint64 anyway
func getUint32(input *[]byte, offset int) (uint64, int) {
	return uint64((*input)[offset+0])<<24 |
		uint64((*input)[offset+1])<<16 |
		uint64((*input)[offset+2])<<8 |
		uint64((*input)[offset+3]), 5
}

func getUint64(input *[]byte, offset int) (uint64, int) {
	return uint64((*input)[offset+0])<<56 |
		uint64((*input)[offset+1])<<48 |
		uint64((*input)[offset+2])<<40 |
		uint64((*input)[offset+3])<<36 |
		uint64((*input)[offset+4])<<24 |
		uint64((*input)[offset+5])<<16 |
		uint64((*input)[offset+6])<<8 |
		uint64((*input)[offset+7]), 8
}

func getStr16(input *[]byte, offset int) (string, int) {
	length := int(uint32((*input)[offset+1])<<8 | uint32((*input)[offset+2]))
	if length == 0 {
		return "", 3
	}
	return string((*input)[offset+3 : offset+3+length]), length + 3
}

func getarray(input *[]byte, offset int) ([]interface{}, int) {
	length := int((*input)[offset] & 0xf)
	initialoffset := offset
	offset += 1
	if length == 0 {
		return nil, 1
	}
	var ret []interface{}
	for arridx := 0; arridx < length; arridx++ {
		var value interface{}
		consumed := 0
		if isstring((*input)[offset]) {
			value, consumed = getstring(input, offset)
		} else if isarray((*input)[offset]) {
			value, consumed = getarray(input, offset)
		} else if (*input)[offset] < 0x7f { // positive fixint
			value = uint64((*input)[offset])
			consumed = 1
		} else if (*input)[offset] == 0xcf { //uint64
			offset += 1
			value, consumed = getUint64(input, offset)
		} else { // is a number, probably
			switch (*input)[offset] {
			case 0xce:
				offset += 1
				value, consumed = getUint32(input, offset)
			default:
				fmt.Println("don't know what this is", (*input)[offset])
			}
		}
		offset += consumed
		ret = append(ret, value)
	}
	return ret, offset - initialoffset
}

func getmap(input *[]byte, offset int) (map[string]interface{}, int) {
	length := int((*input)[offset] & 0xf)
	initialoffset := offset
	offset += 1
	if length == 0 {
		return nil, 1
	}
	ret := map[string]interface{}{}
	for mapidx := 0; mapidx < length; mapidx++ {
		var value interface{}
		var consumed int
		// get key, assuming is string
		key, consumed := getstring(input, offset)
		offset += consumed
		// get value
		if isstring((*input)[offset]) {
			value, consumed = getstring(input, offset)
		} else if isarray((*input)[offset]) {
			value, consumed = getarray(input, offset)
		} else if ismap((*input)[offset]) {
			value, consumed = getmap(input, offset)
		} else if (*input)[offset] == 0xda {
			value, consumed = getStr16(input, offset)
		} else if (*input)[offset] == 0xcf { //uint64
			value, consumed = getUint64(input, offset)
		} else {
			fmt.Println("actualy is dolan", (*input)[offset], offset)
		}
		offset += consumed
		ret[key] = value
	}
	return ret, offset - initialoffset
}

func decode(input []byte) ([]byte, map[string]interface{}) {
	idx := 0 // index into array
	// get wrapper byte: 0x8n
	// length * 2 is number of elements
	if ismap(input[idx]) {
		mymap, consumed := getmap(&input, idx)
		idx += consumed
		return input[idx:], mymap
	} else {
		fmt.Println("unrecognized beginning:", input[idx])
		return input, nil
	}
}