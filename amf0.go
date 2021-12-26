// Encoding / Decoding for AMF0

package main

import (
	"encoding/binary"
	"math"
	"strconv"
)

// Types
const AMF0_TYPE_NUMBER = 0x00
const AMF0_TYPE_BOOL = 0x01
const AMF0_TYPE_STRING = 0x02
const AMF0_TYPE_OBJECT = 0x03
const AMF0_TYPE_NULL = 0x05
const AMF0_TYPE_UNDEFINED = 0x06
const AMF0_TYPE_REF = 0x07
const AMF0_TYPE_ARRAY = 0x08
const AMF0_TYPE_STRICT_ARRAY = 0x0A
const AMF0_TYPE_DATE = 0x0B
const AMF0_TYPE_LONG_STRING = 0x0C
const AMF0_TYPE_XML_DOC = 0x0F
const AMF0_TYPE_TYPED_OBJ = 0x10
const AMF0_TYPE_SWITCH_AMF3 = 0x11

const AMF0_OBJECT_TERM_CODE = 0x09

type AMF0Value struct {
	amf_type  byte
	bool_val  bool
	str_val   string
	int_val   int64
	float_val float64
	obj_val   map[string]AMF0Value
	array_val []AMF0Value
}

func (v AMF0Value) SetFloatVal(val float64) {
	v.float_val = val
	v.int_val = int64(math.Float64bits(val))
}

func (v AMF0Value) SetIntegerVal(val int64) {
	v.int_val = val
	v.float_val = math.Float64frombits(uint64(val))
}

func createAMF0Value(amf_type byte) AMF0Value {
	return AMF0Value{
		amf_type:  amf_type,
		bool_val:  false,
		str_val:   "",
		int_val:   0,
		float_val: 0,
		obj_val:   make(map[string]AMF0Value),
		array_val: make([]AMF0Value, 0),
	}
}

/* Encoding */

func amf0EncodeOne(val AMF0Value) []byte {
	var result []byte

	result = []byte{val.amf_type}

	switch val.amf_type {
	case AMF0_TYPE_NUMBER:
		result = append(result, amf0EncodeNumber(val.float_val)...)
	case AMF0_TYPE_BOOL:
		result = append(result, amf0EncodeBool(val.bool_val)...)
	case AMF0_TYPE_DATE:
		result = append(result, amf0EncodeDate(val.float_val)...)
	case AMF0_TYPE_STRING:
		result = append(result, amf0EncodeString(val.str_val)...)
	case AMF0_TYPE_XML_DOC:
		result = append(result, amf0EncodeString(val.str_val)...)
	case AMF0_TYPE_LONG_STRING:
		result = append(result, amf0EncodeString(val.str_val)...)
	case AMF0_TYPE_OBJECT:
		result = append(result, amf0EncodeObject(val.obj_val)...)
	case AMF0_TYPE_REF:
		result = append(result, amf0EncodeRef(uint16(val.int_val))...)
	case AMF0_TYPE_ARRAY:
		result = append(result, amf0EncodeArray(val.array_val)...)
	case AMF0_TYPE_STRICT_ARRAY:
		result = append(result, amf0EncodeStrictArray(val.array_val)...)
	case AMF0_TYPE_TYPED_OBJ:
		result = append(result, amf0EncodeTypedObject(val.str_val, val.obj_val)...)
	}

	return result
}

func amf0EncodeNumber(num float64) []byte {
	b := make([]byte, 8)
	i := math.Float64bits(num)
	binary.BigEndian.PutUint64(b, i)
	return b
}

func amf0EncodeBool(b bool) []byte {
	if b {
		return []byte{0x01}
	} else {
		return []byte{0x00}
	}
}

func amf0EncodeDate(date float64) []byte {
	return append([]byte{0x00, 0x00}, amf0EncodeNumber(date)...)
}

func amf0EncodeString(str string) []byte {
	b := []byte(str)
	l := make([]byte, 2)
	binary.BigEndian.PutUint16(l, uint16(len(b)))
	return append(l, b...)
}

func amf0EncodeLongString(str string) []byte {
	b := []byte(str)
	l := make([]byte, 4)
	binary.BigEndian.PutUint32(l, uint32(len(b)))
	return append(l, b...)
}

func amf0EncodeObject(o map[string]AMF0Value) []byte {
	var r []byte
	r = make([]byte, 0)

	for key, element := range o {
		r = append(r, amf0EncodeString(key)...)
		r = append(r, amf0EncodeOne(element)...)
	}

	r = append(r, []byte{AMF0_OBJECT_TERM_CODE}...)

	return r
}

func amf0EncodeArray(array []AMF0Value) []byte {
	// Length
	var r []byte
	r = make([]byte, 4)
	binary.BigEndian.PutUint32(r, uint32(len(array)))

	// Values
	var o map[string]AMF0Value
	o = make(map[string]AMF0Value)

	for i := 0; i < len(array); i++ {
		o[string(i)] = array[i]
	}

	return append(r, amf0EncodeObject(o)...)
}

func amf0EncodeStrictArray(array []AMF0Value) []byte {
	// Length
	var r []byte
	r = make([]byte, 4)
	binary.BigEndian.PutUint32(r, uint32(len(array)))

	for i := 0; i < len(array); i++ {
		r = append(r, amf0EncodeOne(array[i])...)
	}

	return r
}

func amf0EncodeRef(index uint16) []byte {
	l := make([]byte, 2)
	binary.BigEndian.PutUint16(l, index)
	return l
}

func amf0EncodeTypedObject(className string, o map[string]AMF0Value) []byte {
	var r []byte
	r = amf0EncodeString(className)
	return append(r, amf0EncodeObject(o)...)
}

/* Decoding */

type AMF0DecodingStream struct {
	buffer []byte
	pos    int
}

func (s AMF0DecodingStream) Read(n int) []byte {
	r := s.buffer[s.pos:(s.pos + n)]
	s.pos += n
	return r
}

func (s AMF0DecodingStream) Look(n int) []byte {
	r := s.buffer[s.pos:(s.pos + n)]
	return r
}

func (s AMF0DecodingStream) Skip(n int) {
	s.pos += n
}

func (s AMF0DecodingStream) IsEnded() bool {
	return s.pos < len(s.buffer)
}

func (s AMF0DecodingStream) ReadOne() AMF0Value {
	amf_type := s.Read(1)[0]
	r := createAMF0Value(amf_type)
	switch amf_type {
	case AMF0_TYPE_NUMBER:
		r.SetFloatVal(s.ReadNumber())
	case AMF0_TYPE_BOOL:
		r.bool_val = s.ReadBool()
	case AMF0_TYPE_DATE:
		s.Skip(2)
		r.SetFloatVal(s.ReadNumber())
	case AMF0_TYPE_STRING:
		r.str_val = s.ReadString()
	case AMF0_TYPE_XML_DOC:
		r.str_val = s.ReadString()
	case AMF0_TYPE_LONG_STRING:
		r.str_val = s.ReadLongString()
	case AMF0_TYPE_OBJECT:
		r.obj_val = s.ReadObject()
	case AMF0_TYPE_TYPED_OBJ:
		r.str_val, r.obj_val = s.ReadTypedObject()
	case AMF0_TYPE_REF:
		s.Skip(2)
	case AMF0_TYPE_ARRAY:
		r.array_val = s.ReadArray()
	case AMF0_TYPE_STRICT_ARRAY:
		r.array_val = s.ReadStrictArray()
	}
	return r
}

func (s AMF0DecodingStream) ReadNumber() float64 {
	buf := s.Read(8)
	a := binary.BigEndian.Uint64(buf)
	return math.Float64frombits(a)
}

func (s AMF0DecodingStream) ReadBool() bool {
	buf := s.Read(1)
	return buf[0] != 0x00
}

func (s AMF0DecodingStream) ReadString() string {
	l := binary.BigEndian.Uint16(s.Read(2))
	strBytes := s.Read(int(l))
	return string(strBytes)
}

func (s AMF0DecodingStream) ReadLongString() string {
	l := binary.BigEndian.Uint32(s.Read(4))
	strBytes := s.Read(int(l))
	return string(strBytes)
}

func (s AMF0DecodingStream) ReadObject() map[string]AMF0Value {
	o := make(map[string]AMF0Value)

	for !s.IsEnded() && s.Look(1)[0] != AMF0_OBJECT_TERM_CODE {
		propName := s.ReadString()

		if s.Look(1)[0] != AMF0_OBJECT_TERM_CODE {
			propVal := s.ReadOne()
			o[propName] = propVal
		}
	}

	return o
}

func (s AMF0DecodingStream) ReadArray() []AMF0Value {
	s.Skip(4)
	o := s.ReadObject()
	r := make([]AMF0Value, len(o))

	for i := 0; i < len(r); i++ {
		r[i] = createAMF0Value(AMF0_TYPE_UNDEFINED)
	}

	for key, value := range o {
		v, e := strconv.Atoi(key)
		if e != nil {
			if v >= 0 && v < len(r) {
				r[v] = value
			}
		}
	}

	return r
}

func (s AMF0DecodingStream) ReadStrictArray() []AMF0Value {
	var r []AMF0Value
	r = make([]AMF0Value, 0)

	l := binary.BigEndian.Uint32(s.Read(4))

	for i := uint32(0); i < l && !s.IsEnded(); i++ {
		r = append(r, s.ReadOne())
	}

	return r
}

func (s AMF0DecodingStream) ReadTypedObject() (string, map[string]AMF0Value) {
	className := s.ReadString()
	o := s.ReadObject()
	return className, o
}
