// RTMP packet

package main

import (
	"encoding/binary"
)

// The header of a RTMP packet
type RTMPPacketHeader struct {
	timestamp int64 // Timestamp of the packet

	fmt uint32 // Packet format

	cid uint32 // Packet channel ID

	packet_type uint32 // Packet type

	stream_id uint32 // Packet Stream ID

	length uint32 // Payload length
}

// Represents a RTMP packet
type RTMPPacket struct {
	header RTMPPacketHeader // Header (metadata)
	clock  int64            // Used for extended timestamp

	capacity uint32 // Current packet capacity
	bytes    uint32 // Current packet size
	handled  bool   // True if the packet was handled

	payload []byte // Packet payload
}

const RTMP_PACKET_BASE_SIZE = 65

// Creates and returns a blank RTMP packet
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
		handled:  false,
	}
}

// Serializes a basic header for a RTMP packet
// fmt - Packet format
// cid - Packet channel ID
// Returns the serialized bytes
func rtmpChunkBasicHeaderCreate(fmt uint32, cid uint32) []byte {
	var out []byte

	if cid >= 64+255 {
		out = make([]byte, 3)
		out[0] = byte(fmt<<6) | 1
		out[1] = byte(cid-64) & 0xff
		out[2] = byte(cid-64>>8) & 0xff
	} else if cid >= 64 {
		out = make([]byte, 2)
		out[0] = byte(fmt << 6)
		out[1] = byte(cid-64) & 0xff
	} else {
		out = make([]byte, 1)
		out[0] = byte(fmt<<6) | byte(cid)
	}

	return out
}

// Serializes the header of a RTMP packet
// packet - The packet
// Returns the serialized bytes
func rtmpChunkMessageHeaderCreate(packet *RTMPPacket) []byte {
	out := make([]byte, 0)

	if packet.header.fmt <= RTMP_CHUNK_TYPE_2 {
		b := make([]byte, 4)
		if packet.header.timestamp >= 0xffffff {
			binary.BigEndian.PutUint32(b, 0xffffff)
		} else {
			binary.BigEndian.PutUint32(b, uint32(packet.header.timestamp))
		}
		out = append(out, b[1:]...)
	}

	if packet.header.fmt <= RTMP_CHUNK_TYPE_1 {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, packet.header.length)
		out = append(out, b[1:]...)

		out = append(out, byte(packet.header.packet_type))
	}

	if packet.header.fmt == RTMP_CHUNK_TYPE_0 {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, packet.header.stream_id)
		out = append(out, b...)
	}

	return out
}

// Creates the chunks to send for a RTMP packet
// outChunkSize - The chunk size
// Returns the serialized chunks
func (packet *RTMPPacket) CreateChunks(outChunkSize int) []byte {
	chunkBasicHeader := rtmpChunkBasicHeaderCreate(packet.header.fmt, packet.header.cid)
	chunkBasicHeader3 := rtmpChunkBasicHeaderCreate(RTMP_CHUNK_TYPE_3, packet.header.cid)

	chunkMessageHeader := rtmpChunkMessageHeaderCreate(packet)

	useExtendedTimestamp := (packet.header.timestamp >= 0xffffff)

	var headerSize int
	var payloadSize int
	var chunksOffset int
	var payloadOffset int
	var n int

	headerSize = len(chunkBasicHeader) + len(chunkMessageHeader)
	payloadSize = int(packet.header.length)
	chunksOffset = 0
	payloadOffset = 0

	if useExtendedTimestamp {
		headerSize += 4
	}

	n = headerSize + payloadSize + (payloadSize / outChunkSize)

	if useExtendedTimestamp {
		n += (payloadSize / outChunkSize) * 4
	}

	if (payloadSize % outChunkSize) == 0 {
		n--
		if useExtendedTimestamp {
			n -= 4
		}
	}

	chunks := make([]byte, n)

	copy(chunks[chunksOffset:], chunkBasicHeader[:])
	chunksOffset += len(chunkBasicHeader)

	copy(chunks[chunksOffset:], chunkMessageHeader[:])
	chunksOffset += len(chunkMessageHeader)

	if useExtendedTimestamp {
		binary.BigEndian.PutUint32(chunks[chunksOffset:chunksOffset+4], uint32(packet.header.timestamp))
		chunksOffset += 4
	}

	for payloadSize > 0 {
		if payloadSize > outChunkSize {
			copy(chunks[chunksOffset:], packet.payload[payloadOffset:payloadOffset+outChunkSize])
			payloadSize -= outChunkSize
			chunksOffset += outChunkSize
			payloadOffset += outChunkSize
			copy(chunks[chunksOffset:], chunkBasicHeader3[:])
			chunksOffset += len(chunkBasicHeader3)
			if useExtendedTimestamp {
				binary.BigEndian.PutUint32(chunks[chunksOffset:chunksOffset+4], uint32(packet.header.timestamp))
				chunksOffset += 4
			}
		} else {
			copy(chunks[chunksOffset:], packet.payload[payloadOffset:payloadOffset+payloadSize])
			payloadSize -= payloadSize
			chunksOffset += payloadSize
			payloadOffset += payloadSize
		}
	}

	//LogDebug("PAYLOAD: " + hex.EncodeToString(packet.payload))
	//LogDebug("CHUNKS: " + hex.EncodeToString(chunks))

	return chunks
}
