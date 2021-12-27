// Encoding / Decoding for AMF3

package main

import (
	"encoding/binary"
	"math"
)

// Types
const AMF3_TYPE_UNDEFINED = 0x00
const AMF3_TYPE_NULL = 0x01
const AMF3_TYPE_FALSE = 0x02
const AMF3_TYPE_TRUE = 0x03
const AMF3_TYPE_INTEGER = 0x04
const AMF3_TYPE_DOUBLE = 0x05
const AMF3_TYPE_STRING = 0x06
const AMF3_TYPE_XML_DOC = 0x07
const AMF3_TYPE_DATE = 0x08
const AMF3_TYPE_ARRAY = 0x09
const AMF3_TYPE_OBJECT = 0x0A
const AMF3_TYPE_XML = 0x0B
const AMF3_TYPE_BYTE_ARRAY = 0x0C

type AMF3Value struct {
	amf_type  byte
	int_val   int32
	float_val float64
	str_val   string
	bytes_val []byte
}

func createAMF3Value(amf_type byte) AMF3Value {
	return AMF3Value{
		amf_type:  amf_type,
		int_val:   0,
		float_val: 0,
		str_val:   "",
		bytes_val: make([]byte, 0),
	}
}

func (v *AMF3Value) GetBool() bool {
	return v.amf_type == AMF3_TYPE_TRUE
}

/* Encoding */

func amf3encUI29(num uint32) []byte {
	var buf []byte
	if num < 0x80 {
		buf = make([]byte, 1)
		buf[0] = byte(num)
	} else if num < 0x4000 {
		buf = make([]byte, 2)
		buf[0] = byte(num & 0x7F)
		buf[1] = byte((num >> 7) | 0x80)
	} else if num < 0x200000 {
		buf = make([]byte, 3)
		buf[0] = byte(num & 0x7F)
		buf[1] = byte((num >> 7) & 0x7F)
		buf[2] = byte((num >> 14) | 0x80)
	} else {
		buf = make([]byte, 4)
		buf[0] = byte(num & 0xFF)
		buf[1] = byte((num >> 8) & 0x7F)
		buf[2] = byte((num >> 15) | 0x7F)
		buf[3] = byte((num >> 22) | 0x7F)
	}
	return buf
}

func amf3EncodeOne(val AMF3Value) []byte {
	var result []byte

	result = []byte{val.amf_type}

	switch val.amf_type {
	case AMF3_TYPE_INTEGER:
		result = append(result, amf3EncodeInteger(val.int_val)...)
	case AMF3_TYPE_DOUBLE:
		result = append(result, amf3EncodeDouble(val.float_val)...)
	case AMF3_TYPE_STRING:
		result = append(result, amf3EncodeString(val.str_val)...)
	case AMF3_TYPE_XML:
		result = append(result, amf3EncodeString(val.str_val)...)
	case AMF3_TYPE_XML_DOC:
		result = append(result, amf3EncodeString(val.str_val)...)
	case AMF3_TYPE_DATE:
		result = append(result, amf3EncodeDate(val.float_val)...)
	case AMF3_TYPE_BYTE_ARRAY:
		result = append(result, amf3EncodeByteArray(val.bytes_val)...)
	}

	return result
}

func amf3EncodeString(str string) []byte {
	b := []byte(str)
	sLen := amf3encUI29(uint32(len(b)) << 1)
	return append(sLen, b...)
}

func amf3EncodeInteger(i int32) []byte {
	return amf3encUI29(uint32(i) & 0x3FFFFFFF)
}

func amf3EncodeDouble(d float64) []byte {
	b := make([]byte, 8)
	i := math.Float64bits(d)
	binary.BigEndian.PutUint64(b, i)
	return b
}

func amf3EncodeDate(ts float64) []byte {
	prefix := amf3encUI29(1)
	return append(prefix, amf3EncodeDouble(ts)...)
}

func amf3EncodeByteArray(b []byte) []byte {
	sLen := amf3encUI29(uint32(len(b)) << 1)
	return append(sLen, b...)
}

/* Decoding */

func (s *AMFDecodingStream) amf3decUI29() uint32 {
	var val uint32
	var len uint32
	var ended bool
	var b byte

	val = 0
	len = 1
	ended = false

	for !ended {
		b = s.Read(1)[0]
		len++
		val = (val << 7) + uint32(b&0x7F)

		if len < 5 || b > 0x7F {
			ended = true
		}
	}

	if len == 5 {
		val = val | uint32(b)
	}

	return val
}

func (s *AMFDecodingStream) ReadAMF3() AMF3Value {
	amf_type := s.Read(1)[0]
	r := createAMF3Value(amf_type)
	switch amf_type {
	case AMF3_TYPE_INTEGER:
		r.int_val = int32(s.amf3decUI29())
	case AMF3_TYPE_DOUBLE:
		r.float_val = s.ReadNumber()
	case AMF3_TYPE_DATE:
		r.int_val = int32(s.amf3decUI29())
		r.float_val = s.ReadNumber()
	case AMF3_TYPE_STRING:
		r.str_val = s.ReadAMF3String()
	case AMF3_TYPE_XML:
		r.str_val = s.ReadAMF3String()
	case AMF3_TYPE_XML_DOC:
		r.str_val = s.ReadAMF3String()
	case AMF3_TYPE_BYTE_ARRAY:
		r.bytes_val = s.ReadAMF3ByteArray()
	}
	return r
}

func (s *AMFDecodingStream) ReadAMF3String() string {
	l := s.amf3decUI29()
	strBytes := s.Read(int(l))
	return string(strBytes)
}

func (s *AMFDecodingStream) ReadAMF3ByteArray() []byte {
	l := s.amf3decUI29()
	strBytes := s.Read(int(l))
	return strBytes
}
