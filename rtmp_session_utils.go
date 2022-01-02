// RTMP session utils

package main

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"time"

	"github.com/netdata/go.d.plugin/pkg/iprange"
)

func (s *RTMPSession) SendACK(size uint32) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x04, 0x03,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	binary.BigEndian.PutUint32(b[12:16], size)

	s.SendSync(b)

	return true
}

func (s *RTMPSession) SendWindowACK(size uint32) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x04, 0x05,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	binary.BigEndian.PutUint32(b[12:16], size)

	s.SendSync(b)

	return true
}

func (s *RTMPSession) SetPeerBandwidth(size uint32, t byte) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x05, 0x06,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00,
	}

	binary.BigEndian.PutUint32(b[12:16], size)

	b[16] = t

	s.SendSync(b)

	return true
}

func (s *RTMPSession) SetChunkSize(size uint32) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x04, 0x01,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	binary.BigEndian.PutUint32(b[12:16], size)

	s.SendSync(b)

	return true
}

func (s *RTMPSession) SendStreamStatus(st uint16, id uint32) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x06, 0x04,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	binary.BigEndian.PutUint16(b[12:14], st)
	binary.BigEndian.PutUint32(b[14:18], id)

	s.SendSync(b)

	return true
}

func (s *RTMPSession) SendPingRequest() {
	if !s.isConnected {
		return
	}

	now := time.Now().UnixMilli()
	currentTimestamp := now - s.connectTime
	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_PROTOCOL
	packet.header.packet_type = RTMP_TYPE_EVENT
	packet.header.timestamp = currentTimestamp

	packet.payload = []byte{
		0,
		6,
		byte(currentTimestamp>>24) & 0xff,
		byte(currentTimestamp>>16) & 0xff,
		byte(currentTimestamp>>8) & 0xff,
		byte(currentTimestamp) & 0xff,
	}

	packet.header.length = uint32(len(packet.payload))

	bytes := packet.CreateChunks(int(s.outChunkSize))
	LogDebugSession(s.id, s.ip, "Sending ping request")
	s.SendSync(bytes)
}

func (s *RTMPSession) SendInvokeMessage(stream_id uint32, cmd RTMPCommand) {
	packet := createBlankRTMPPacket()

	LogDebugSession(s.id, s.ip, "Sending invoke message: "+cmd.ToString())

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_INVOKE
	packet.header.packet_type = RTMP_TYPE_INVOKE
	packet.header.stream_id = stream_id
	packet.payload = cmd.Encode()
	packet.header.length = uint32(len(packet.payload))

	bytes := packet.CreateChunks(int(s.outChunkSize))
	s.SendSync(bytes)
}

func (s *RTMPSession) SendDataMessage(stream_id uint32, data RTMPData) {
	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_DATA
	packet.header.packet_type = RTMP_TYPE_DATA
	packet.header.stream_id = stream_id
	packet.payload = data.Encode()
	packet.header.length = uint32(len(packet.payload))

	bytes := packet.CreateChunks(int(s.outChunkSize))
	s.SendSync(bytes)
}

func (s *RTMPSession) SendStatusMessage(stream_id uint32, level string, code string, description string) {
	cmd := RTMPCommand{
		cmd:       "onStatus",
		arguments: make(map[string]*AMF0Value),
	}

	transId := createAMF0Value(AMF0_TYPE_NUMBER)
	transId.SetIntegerVal(0)
	cmd.arguments["transId"] = &transId

	cmdObj := createAMF0Value(AMF0_TYPE_NULL)
	cmd.arguments["cmdObj"] = &cmdObj

	info := createAMF0Value(AMF0_TYPE_OBJECT)

	info_level := createAMF0Value(AMF0_TYPE_STRING)
	info_level.str_val = level
	info.obj_val["level"] = &info_level

	info_code := createAMF0Value(AMF0_TYPE_STRING)
	info_code.str_val = code
	info.obj_val["code"] = &info_code

	if description != "" {
		info_description := createAMF0Value(AMF0_TYPE_STRING)
		info_description.str_val = description
		info.obj_val["description"] = &info_description
	}

	cmd.arguments["info"] = &info

	s.SendInvokeMessage(stream_id, cmd)
}

func (s *RTMPSession) SendSampleAccess(stream_id uint32) {
	cmd := RTMPData{
		tag:       "|RtmpSampleAccess",
		arguments: make(map[string]*AMF0Value),
	}

	bool1 := createAMF0Value(AMF0_TYPE_BOOL)
	bool1.bool_val = false
	cmd.arguments["bool1"] = &bool1

	bool2 := createAMF0Value(AMF0_TYPE_BOOL)
	bool2.bool_val = false
	cmd.arguments["bool2"] = &bool2

	s.SendDataMessage(stream_id, cmd)
}

func (s *RTMPSession) RespondConnect(tid int64, hasObjectEncoding bool) {
	cmd := RTMPCommand{
		cmd:       "_result",
		arguments: make(map[string]*AMF0Value),
	}

	transId := createAMF0Value(AMF0_TYPE_NUMBER)
	transId.SetIntegerVal(tid)
	cmd.arguments["transId"] = &transId

	cmdObj := createAMF0Value(AMF0_TYPE_OBJECT)

	fmsVer := createAMF0Value(AMF0_TYPE_STRING)
	fmsVer.str_val = "FMS/3,0,1,123"
	cmdObj.obj_val["fmsVer"] = &fmsVer

	capabilities := createAMF0Value(AMF0_TYPE_NUMBER)
	capabilities.SetIntegerVal(31)
	cmdObj.obj_val["capabilities"] = &capabilities

	cmd.arguments["cmdObj"] = &cmdObj

	info := createAMF0Value(AMF0_TYPE_OBJECT)

	info_level := createAMF0Value(AMF0_TYPE_STRING)
	info_level.str_val = "status"
	info.obj_val["level"] = &info_level

	info_code := createAMF0Value(AMF0_TYPE_STRING)
	info_code.str_val = "NetConnection.Connect.Success"
	info.obj_val["code"] = &info_code

	info_description := createAMF0Value(AMF0_TYPE_STRING)
	info_description.str_val = "Connection succeeded."
	info.obj_val["description"] = &info_description

	if hasObjectEncoding {
		objectEncoding := createAMF0Value(AMF0_TYPE_NUMBER)
		objectEncoding.SetIntegerVal(int64(s.objectEncoding))
		info.obj_val["objectEncoding"] = &objectEncoding
	} else {
		objectEncoding := createAMF0Value(AMF0_TYPE_UNDEFINED)
		info.obj_val["objectEncoding"] = &objectEncoding
	}

	cmd.arguments["info"] = &info

	s.SendInvokeMessage(0, cmd)
}

func (s *RTMPSession) RespondCreateStream(tid int64) {
	cmd := RTMPCommand{
		cmd:       "_result",
		arguments: make(map[string]*AMF0Value),
	}

	transId := createAMF0Value(AMF0_TYPE_NUMBER)
	transId.SetIntegerVal(tid)
	cmd.arguments["transId"] = &transId

	cmdObj := createAMF0Value(AMF0_TYPE_NULL)
	cmd.arguments["cmdObj"] = &cmdObj

	s.streams++

	info := createAMF0Value(AMF0_TYPE_NUMBER)
	info.SetIntegerVal(int64(s.streams))
	cmd.arguments["info"] = &info

	s.SendInvokeMessage(0, cmd)
}

func (s *RTMPSession) RespondPlay() {
	s.SendStreamStatus(STREAM_BEGIN, s.playStreamId)
	s.SendStatusMessage(s.playStreamId, "status", "NetStream.Play.Reset", "Playing and resetting stream.")
	s.SendStatusMessage(s.playStreamId, "status", "NetStream.Play.Start", "Started playing stream.")
	s.SendSampleAccess(0)
}

func (s *RTMPSession) SendMetadata(metaData []byte, timestamp int64) {
	if len(metaData) == 0 {
		return
	}

	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_DATA
	packet.header.packet_type = RTMP_TYPE_DATA
	packet.payload = metaData
	packet.header.length = uint32(len(packet.payload))
	packet.header.stream_id = s.playStreamId
	packet.header.timestamp = timestamp

	chunks := packet.CreateChunks(int(s.outChunkSize))

	LogDebugSession(s.id, s.ip, "Send meta data")

	s.SendSync(chunks)
}

func (s *RTMPSession) SendAudioCodecHeader(audioCodec uint32, aacSequenceHeader []byte, timestamp int64) {
	if audioCodec != 10 && audioCodec != 13 {
		return
	}

	LogDebugSession(s.id, s.ip, "Send AUDIO codec header")

	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_AUDIO
	packet.header.packet_type = RTMP_TYPE_AUDIO
	packet.payload = aacSequenceHeader
	packet.header.length = uint32(len(packet.payload))
	packet.header.stream_id = s.playStreamId
	packet.header.timestamp = timestamp

	chunks := packet.CreateChunks(int(s.outChunkSize))

	s.SendSync(chunks)
}

func (s *RTMPSession) SendVideoCodecHeader(videoCodec uint32, avcSequenceHeader []byte, timestamp int64) {
	if videoCodec != 7 && videoCodec != 12 {
		return
	}

	LogDebugSession(s.id, s.ip, "Send VIDEO codec header")

	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_VIDEO
	packet.header.packet_type = RTMP_TYPE_VIDEO
	packet.payload = avcSequenceHeader
	packet.header.length = uint32(len(packet.payload))
	packet.header.stream_id = s.playStreamId
	packet.header.timestamp = timestamp

	chunks := packet.CreateChunks(int(s.outChunkSize))

	s.SendSync(chunks)
}

func (s *RTMPSession) BuildMetadata(data *RTMPData) []byte {
	cmd := RTMPData{
		tag:       "onMetaData",
		arguments: make(map[string]*AMF0Value),
	}

	cmd.arguments["dataObj"] = data.GetArg("dataObj")

	return cmd.Encode()
}

func (s *RTMPSession) SendCachePacket(cache *RTMPPacket) {
	packet := createBlankRTMPPacket()

	packet.header.fmt = cache.header.fmt
	packet.header.cid = cache.header.cid
	packet.header.packet_type = cache.header.packet_type
	packet.payload = cache.payload
	packet.header.length = uint32(len(packet.payload))
	packet.header.stream_id = s.playStreamId
	packet.header.timestamp = cache.header.timestamp

	chunks := packet.CreateChunks(int(s.outChunkSize))

	s.SendSync(chunks)
}

func (s *RTMPSession) CanPlay() bool {
	r := os.Getenv("RTMP_PLAY_WHITELIST")

	if r == "" || r == "*" {
		return true
	}

	ip := net.ParseIP(s.ip)

	parts := strings.Split(r, ",")

	for i := 0; i < len(parts); i++ {
		rang, e := iprange.ParseRange(parts[i])

		if e != nil {
			LogError(e)
			continue
		}

		if rang.Contains(ip) {
			return true
		}
	}

	return false
}
