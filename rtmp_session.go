// RTMP session

package main

import (
	"bufio"
	"encoding/binary"
	"math"
	"net"
	"sync"
	"time"
)

type BitrateCache struct {
	intervalMs  int64
	last_update int64
	bytes       uint64
}

type RTMPSession struct {
	server *RTMPServer
	conn   net.Conn
	ip     string
	mutex  *sync.Mutex

	id          uint64
	inChunkSize uint32

	inPackets map[uint32]*RTMPPacket

	ackSize   uint32
	inAckSize uint32
	inLastAck uint32

	bitrate       uint64
	bitrate_cache BitrateCache
}

func CreateRTMPSession(server *RTMPServer, id uint64, ip string, c net.Conn) RTMPSession {
	return RTMPSession{
		server:      server,
		conn:        c,
		ip:          ip,
		mutex:       &sync.Mutex{},
		id:          id,
		inChunkSize: RTMP_CHUNK_SIZE,
		inPackets:   make(map[uint32]*RTMPPacket),
		ackSize:     0,
		inAckSize:   0,
		inLastAck:   0,

		bitrate: 0,
		bitrate_cache: BitrateCache{
			intervalMs:  1000,
			last_update: 0,
			bytes:       0,
		},
	}
}

func (s *RTMPSession) SendSync(b []byte) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.conn.Write(b)
}

func (s *RTMPSession) HandleSession() {
	r := bufio.NewReader(s.conn)

	err := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
	if err != nil {
		return
	}

	// Handshake

	version, e := r.ReadByte()
	if e != nil {
		return
	}

	if version != RTMP_VERSION {
		LogDebugSession(s.id, s.ip, "Invalid protocol version received")
		return
	}

	handshakeBytes := make([]byte, RTMP_HANDSHAKE_SIZE)
	n, e := r.Read(handshakeBytes)
	if e != nil || n != RTMP_HANDSHAKE_SIZE {
		LogDebugSession(s.id, s.ip, "Invalid handshake received")
		return
	}

	s0s1s2 := generateS0S1S2(handshakeBytes)
	n, e = s.conn.Write(s0s1s2)
	if e != nil || n != RTMP_HANDSHAKE_SIZE {
		LogDebugSession(s.id, s.ip, "Could not send handshake message")
		return
	}

	s1Copy := make([]byte, RTMP_HANDSHAKE_SIZE)
	n, e = r.Read(s1Copy)
	if e != nil || n != RTMP_HANDSHAKE_SIZE {
		LogDebugSession(s.id, s.ip, "Invalid hanshake response received")
		return
	}

	// Read RTMP chunks
	for {
		if !s.ReadChunk(r) {
			return
		}
	}
}

func (s *RTMPSession) ReadChunk(r *bufio.Reader) bool {
	var bytesReadCount uint32
	bytesReadCount = 0

	// Start byte
	startByte, e := r.ReadByte()
	bytesReadCount++
	if e != nil {
		return false
	}

	var header []byte
	header = []byte{startByte}

	var parserBasicBytes int
	if 0 == (startByte & 0x3f) {
		parserBasicBytes = 2
	} else if 1 == (startByte & 0x3f) {
		parserBasicBytes = 3
	} else {
		parserBasicBytes = 1
	}

	for i := 1; i < parserBasicBytes; i++ {
		b, e := r.ReadByte()
		bytesReadCount++
		if e != nil {
			return false
		}

		header = append(header, b)
	}

	// Header
	size := int(rtmpHeaderSize[header[0]>>6])
	if size > 0 {
		headerLeft := make([]byte, size)
		n, e := r.Read(headerLeft)
		bytesReadCount += uint32(size)
		if e != nil || n != size {
			return false
		}
		header = append(header, headerLeft...)
	}

	// Parse packet
	var fmt uint32
	var cid uint32
	fmt = uint32(header[0]) >> 6
	switch parserBasicBytes {
	case 2:
		cid = 64 + uint32(header[1])
	case 3:
		cid = (64 + uint32(header[1]) + uint32(header[2])) << 8
	default:
		cid = uint32(header[0] & 0x3f)
	}

	var packet *RTMPPacket

	if val, ok := s.inPackets[cid]; ok {
		packet = val
	} else {
		bp := createBlankRTMPPacket()
		packet = &bp
		s.inPackets[cid] = packet
	}

	packet.header.cid = cid
	packet.header.fmt = fmt

	offset := parserBasicBytes

	// timestamp / delta
	if packet.header.fmt <= RTMP_CHUNK_TYPE_2 {
		tsBytes := header[offset : offset+3]
		tsBytes = append(tsBytes, 0x00)
		packet.header.timestamp = binary.BigEndian.Uint32(tsBytes)
		offset += 3
	}

	// message length + type
	if packet.header.fmt <= RTMP_CHUNK_TYPE_1 {
		tsBytes := header[offset : offset+3]
		tsBytes = append(tsBytes, 0x00)
		packet.header.length = binary.BigEndian.Uint32(tsBytes)
		packet.header.packet_type = uint32(header[offset+3])
		offset += 4
	}

	// Stream ID
	if packet.header.fmt <= RTMP_CHUNK_TYPE_0 {
		tsBytes := header[offset : offset+4]
		packet.header.stream_id = binary.LittleEndian.Uint32(tsBytes)
		offset += 4
	}

	if packet.header.packet_type > RTMP_TYPE_METADATA {
		return false
	}

	// Extended typestamp
	var extended_timestamp uint32
	if packet.header.timestamp == 0xffffff {
		tsBytes := make([]byte, 4)
		n, e := r.Read(tsBytes)
		bytesReadCount += 4
		if e != nil || n != 4 {
			return false
		}
		extended_timestamp = binary.BigEndian.Uint32(tsBytes)
	} else {
		extended_timestamp = packet.header.timestamp
	}

	if packet.bytes == 0 {
		if packet.header.fmt == RTMP_CHUNK_TYPE_0 {
			packet.clock = extended_timestamp
		} else {
			packet.clock += extended_timestamp
		}

		if packet.capacity < packet.header.length {
			packet.capacity = 1024 + packet.header.length
		}
	}

	var sizeToRead uint32
	sizeToRead = s.inChunkSize - (packet.bytes % s.inChunkSize)
	if sizeToRead > (packet.header.length - packet.bytes) {
		sizeToRead = packet.header.length - packet.bytes
	}

	if sizeToRead > 0 {
		bytesToRead := make([]byte, sizeToRead)
		n, e := r.Read(bytesToRead)
		bytesReadCount += sizeToRead
		if e != nil || uint32(n) != sizeToRead {
			return false
		}

		packet.bytes += sizeToRead
		packet.payload = append(packet.payload, bytesToRead...)
	}

	if packet.bytes >= packet.header.length {
		if packet.clock <= 0xffffffff {
			if !s.HandlePacket(packet) {
				return false
			}
		}
	}

	// ACK

	s.inAckSize += bytesReadCount
	if s.inAckSize >= 0xf0000000 {
		s.inAckSize = 0
		s.inLastAck = 0
	}
	if s.ackSize > 0 && s.inAckSize-s.inLastAck >= s.ackSize {
		s.inLastAck = s.inAckSize
		if !s.SendACK(s.inAckSize) {
			return false
		}
	}

	// Bitrate

	now := time.Now().UnixMilli()
	s.bitrate_cache.bytes += uint64(bytesReadCount)
	diff := now - s.bitrate_cache.last_update
	if diff >= s.bitrate_cache.intervalMs {
		s.bitrate = uint64(math.Round(float64(s.bitrate_cache.bytes) * 8 / float64(diff)))
		s.bitrate_cache.bytes = 0
		s.bitrate_cache.last_update = now
	}

	return true
}

func (s *RTMPSession) HandlePacket(packet *RTMPPacket) bool {
	switch packet.header.packet_type {
	default:
		return true
	}
}

func (s *RTMPSession) SendACK(size uint32) bool {
	b := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x04, 0x03,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, size)

	b[12] = n[0]
	b[13] = n[1]
	b[14] = n[2]
	b[15] = n[3]

	nw, err := s.conn.Write(b)

	if err != nil || nw != len(b) {
		return false
	}

	return true
}

func (s *RTMPSession) OnClose() {
}