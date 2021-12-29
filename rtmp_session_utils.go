// RTMP session utils

package main

import (
	"encoding/binary"
	"time"
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

	currentTimestamp := time.Now().UnixMilli() - s.connectTime
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

	bytes := packet.CreateChunks()
	s.SendSync(bytes)
}

func (s *RTMPSession) SendInvokeMessage(stream_id uint32, cmd RTMPCommand) {
	packet := createBlankRTMPPacket()

	packet.header.fmt = RTMP_CHUNK_TYPE_0
	packet.header.cid = RTMP_CHANNEL_INVOKE
	packet.header.packet_type = RTMP_TYPE_INVOKE
	packet.header.stream_id = stream_id
	packet.payload = cmd.Encode()
	packet.header.length = uint32(len(packet.payload))

	bytes := packet.CreateChunks()
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

	bytes := packet.CreateChunks()
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
