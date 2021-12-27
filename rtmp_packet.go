// RTMP packet

package main

type RTMPPacketHeader struct {
	timestamp uint32

	fmt uint32
	cid uint32

	packet_type uint32

	stream_id uint32

	length uint32 // Payload length
}

type RTMPPacket struct {
	header   RTMPPacketHeader
	clock    uint32
	payload  []byte
	capacity uint32
	bytes    uint32
}

func createBlankRTMPPacket() RTMPPacket {
	return RTMPPacket{
		header: RTMPPacketHeader{
			timestamp:   0,
			fmt:         0,
			cid:         0,
			packet_type: 0,
			stream_id:   0,
			length:      0,
		},
		clock:    0,
		payload:  []byte{},
		capacity: 0,
		bytes:    0,
	}
}
