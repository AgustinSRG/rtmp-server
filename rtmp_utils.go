// RTMP server

package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

/* Constants */

const N_CHUNK_STREAM = 8
const RTMP_VERSION = 3
const RTMP_HANDSHAKE_SIZE = 1536
const RTMP_HANDSHAKE_UNINIT = 0
const RTMP_HANDSHAKE_0 = 1
const RTMP_HANDSHAKE_1 = 2
const RTMP_HANDSHAKE_2 = 3

const RTMP_PARSE_INIT = 0
const RTMP_PARSE_BASIC_HEADER = 1
const RTMP_PARSE_MESSAGE_HEADER = 2
const RTMP_PARSE_EXTENDED_TIMESTAMP = 3
const RTMP_PARSE_PAYLOAD = 4

const MAX_CHUNK_HEADER = 18

const RTMP_CHUNK_TYPE_0 = 0 // 11-bytes: timestamp(3) + length(3) + stream type(1) + stream id(4)
const RTMP_CHUNK_TYPE_1 = 1 // 7-bytes: delta(3) + length(3) + stream type(1)
const RTMP_CHUNK_TYPE_2 = 2 // 3-bytes: delta(3)
const RTMP_CHUNK_TYPE_3 = 3 // 0-byte

const RTMP_CHANNEL_PROTOCOL = 2
const RTMP_CHANNEL_INVOKE = 3
const RTMP_CHANNEL_AUDIO = 4
const RTMP_CHANNEL_VIDEO = 5
const RTMP_CHANNEL_DATA = 6

var rtmpHeaderSize = []uint32{11, 7, 3, 0}

/* Protocol Control Messages */
const RTMP_TYPE_SET_CHUNK_SIZE = 1
const RTMP_TYPE_ABORT = 2
const RTMP_TYPE_ACKNOWLEDGEMENT = 3             // bytes read report
const RTMP_TYPE_WINDOW_ACKNOWLEDGEMENT_SIZE = 5 // server bandwidth
const RTMP_TYPE_SET_PEER_BANDWIDTH = 6          // client bandwidth

/* User Control Messages Event (4) */
const RTMP_TYPE_EVENT = 4

const RTMP_TYPE_AUDIO = 8
const RTMP_TYPE_VIDEO = 9

/* Data Message */
const RTMP_TYPE_FLEX_STREAM = 15 // AMF3
const RTMP_TYPE_DATA = 18        // AMF0

/* Shared Object Message */
const RTMP_TYPE_FLEX_OBJECT = 16   // AMF3
const RTMP_TYPE_SHARED_OBJECT = 19 // AMF0

/* Command Message */
const RTMP_TYPE_FLEX_MESSAGE = 17 // AMF3
const RTMP_TYPE_INVOKE = 20       // AMF0

/* Aggregate Message */
const RTMP_TYPE_METADATA = 22

const RTMP_CHUNK_SIZE = 128
const RTMP_PING_TIME = 30000
const RTMP_PING_TIMEOUT = 60000

const STREAM_BEGIN = 0x00
const STREAM_EOF = 0x01
const STREAM_DRY = 0x02
const STREAM_EMPTY = 0x1f
const STREAM_READY = 0x20

var rtmpCmdCode = map[string][]string{
	"_result":         {"transId", "cmdObj", "info"},
	"_error":          {"transId", "cmdObj", "info", "streamId"},
	"onStatus":        {"transId", "cmdObj", "info"},
	"releaseStream":   {"transId", "cmdObj", "streamName"},
	"getStreamLength": {"transId", "cmdObj", "streamId"},
	"getMovLen":       {"transId", "cmdObj", "streamId"},
	"FCPublish":       {"transId", "cmdObj", "streamName"},
	"FCUnpublish":     {"transId", "cmdObj", "streamName"},
	"FCSubscribe":     {"transId", "cmdObj", "streamName"},
	"onFCPublish":     {"transId", "cmdObj", "info"},
	"connect":         {"transId", "cmdObj", "args"},
	"call":            {"transId", "cmdObj", "args"},
	"createStream":    {"transId", "cmdObj"},
	"close":           {"transId", "cmdObj"},
	"play":            {"transId", "cmdObj", "streamName", "start", "duration", "reset"},
	"play2":           {"transId", "cmdObj", "params"},
	"deleteStream":    {"transId", "cmdObj", "streamId"},
	"closeStream":     {"transId", "cmdObj"},
	"receiveAudio":    {"transId", "cmdObj", "bool"},
	"receiveVideo":    {"transId", "cmdObj", "bool"},
	"publish":         {"transId", "cmdObj", "streamName", "type"},
	"seek":            {"transId", "cmdObj", "ms"},
	"pause":           {"transId", "cmdObj", "pause", "ms"},
}

var rtmpDataCode = map[string][]string{
	"@setDataFrame":     {"method", "dataObj"},
	"onFI":              {"info"},
	"onMetaData":        {"dataObj"},
	"|RtmpSampleAccess": {"bool1", "bool2"},
}

type RTMPCommand struct {
	cmd       string
	arguments map[string]*AMF0Value
}

func (c *RTMPCommand) GetArg(argName string) *AMF0Value {
	if c.arguments[argName] != nil {
		return c.arguments[argName]
	} else {
		n := createAMF0Value(AMF0_TYPE_UNDEFINED)
		return &n
	}
}

func (c *RTMPCommand) ToString() string {
	str := "" + c.cmd + " {\n"

	for argName, argVal := range c.arguments {
		str += "    '" + argName + "' = " + argVal.ToString("    ") + "\n"
	}

	str += "}"
	return str
}

func (c *RTMPCommand) Encode() []byte {
	var buf []byte

	x := createAMF0Value(AMF0_TYPE_STRING)
	x.str_val = c.cmd

	buf = amf0EncodeOne(x)

	argList := rtmpCmdCode[c.cmd]

	for i := 0; i < len(argList); i++ {
		val := c.arguments[argList[i]]
		if val != nil {
			buf = append(buf, amf0EncodeOne(*val)...)
		} else {
			buf = append(buf, amf0EncodeOne(createAMF0Value(AMF0_TYPE_UNDEFINED))...)
		}
	}

	return buf
}

func decodeRTMPCommand(data []byte) RTMPCommand {
	c := RTMPCommand{
		cmd:       "",
		arguments: make(map[string]*AMF0Value),
	}
	s := AMFDecodingStream{
		buffer: data,
		pos:    0,
	}

	c.cmd = s.ReadOne().str_val

	argList := rtmpCmdCode[c.cmd]

	for i := 0; i < len(argList) && !s.IsEnded(); i++ {
		val := s.ReadOne()
		c.arguments[argList[i]] = &val
	}

	return c
}

type RTMPData struct {
	tag       string
	arguments map[string]*AMF0Value
}

func (c *RTMPData) ToString() string {
	str := "" + c.tag + " {\n"

	for argName, argVal := range c.arguments {
		str += "    '" + argName + "' = " + argVal.ToString("    ") + "\n"
	}

	str += "}"
	return str
}

func (c *RTMPData) GetArg(argName string) *AMF0Value {
	if c.arguments[argName] != nil {
		return c.arguments[argName]
	} else {
		n := createAMF0Value(AMF0_TYPE_UNDEFINED)
		return &n
	}
}

func (c *RTMPData) Encode() []byte {
	var buf []byte

	x := createAMF0Value(AMF0_TYPE_STRING)
	x.str_val = c.tag

	buf = amf0EncodeOne(x)

	argList := rtmpDataCode[c.tag]

	for i := 0; i < len(argList); i++ {
		val := c.arguments[argList[i]]
		if val != nil {
			buf = append(buf, amf0EncodeOne(*val)...)
		}
	}

	return buf
}

func decodeRTMPData(data []byte) RTMPData {
	c := RTMPData{
		tag:       "",
		arguments: make(map[string]*AMF0Value),
	}
	s := AMFDecodingStream{
		buffer: data,
		pos:    0,
	}

	c.tag = s.ReadOne().str_val

	argList := rtmpDataCode[c.tag]

	for i := 0; i < len(argList) && !s.IsEnded(); i++ {
		val := s.ReadOne()
		c.arguments[argList[i]] = &val
	}

	return c
}

var ID_MAX_LENGTH = 128
var idCustomMaxLength = os.Getenv("ID_MAX_LENGTH")

func validateStreamIDString(str string) bool {
	if idCustomMaxLength != "" {
		var e error
		ID_MAX_LENGTH, e = strconv.Atoi(idCustomMaxLength)
		if e != nil {
			ID_MAX_LENGTH = 128
		}
	}

	if len(str) > ID_MAX_LENGTH {
		return false
	}

	m, e := regexp.MatchString("^[A-Za-z0-9\\_\\-]+$", str)

	if e != nil {
		return false
	}

	return m
}

func getRTMPParamsSimple(str string) map[string]string {
	result := make(map[string]string)

	if len(str) > 0 {
		parts := strings.Split(str, "&")

		for i := 0; i < len(parts); i++ {
			keyVal := strings.Split(parts[i], "=")
			if len(keyVal) == 2 {
				result[keyVal[0]] = result[keyVal[1]]
			}
		}
	}

	return result
}
