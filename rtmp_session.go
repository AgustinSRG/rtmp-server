// RTMP session

package main

import (
	"bufio"
	"container/list"
	"encoding/binary"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Structure to store the bit rate status
type BitRateCache struct {
	intervalMs  int64  // Interval of milliseconds to update
	last_update int64  // Last time updated (unix milliseconds)
	bytes       uint64 // The number of bytes received
}

// Stores the status of a RTMP session
type RTMPSession struct {
	server *RTMPServer // Reference to the server

	conn net.Conn // TCP connection

	id uint64 // Session ID
	ip string // IP address of the client

	inChunkSize  uint32 // Chunk size of incoming packets
	outChunkSize uint32 //  Chunks size for outgoing packets

	ackSize   uint32 // Acknowledge size required by the client
	inAckSize uint32 // Amount of bytes acknowledged
	inLastAck uint32 // This is used to count bytes that must be acknowledged

	objectEncoding uint32 // Encoding format required by the client

	connectTime int64 // Connection time (unix milliseconds)

	mutex *sync.Mutex // Mutex to control access to the session status data

	publish_mutex *sync.Mutex // Mutex to control the publishing group

	inPackets map[uint32]*RTMPPacket // RTMP packets storage. Map: Channel ID -> Packet. Packets are received in chunks, so they are stored until the last chunk is received.

	playStreamId    uint32 // ID of the stream being played
	publishStreamId uint32 // ID of the stream being published
	streams         uint32 // Number of associated streams

	receive_audio bool // True if the client wants to receive audio packets
	receive_video bool // True if the client want to receive video packets

	channel   string // Streaming channel ID
	key       string // Streaming key
	stream_id string // Stream ID

	isConnected  bool // True if the client sent the connect message
	isPublishing bool // True if the client is publishing
	isPlaying    bool // True if the client is playing
	isIdling     bool // True if the client is waiting to play a stream
	isPause      bool // True if the client is paused

	metaData          []byte // Metadata for the stream being published
	audioCodec        uint32 // Audio codec
	videoCodec        uint32 // Video codec
	aacSequenceHeader []byte // Sequence header for AAC codec (Audio)
	avcSequenceHeader []byte // Seque4nce header for AVC codec (Video)

	clock int64 // Current clock value

	rtmpGopCache     *list.List // List to store the GOP cache
	gopCacheSize     int64      // Current GOP cache size
	gopCacheLimit    int64      // GOP cache size limit
	gopCacheDisabled bool       // True if the cache is currently disabled
	gopPlayNo        bool       // True if the client refuses to receive the cache packets
	gopPlayClear     bool       // True if the clients is requesting to clear the cache

	bitRate      uint64       // Bitrate (bit/ms)
	bitRateCache BitRateCache // Cache to compute bit rate
}

// Creates a RTMP session
// server - Server that accepted the connection
// id - Session ID
// ip - Client IP address
// c - TCP connection
// Returns the session
func CreateRTMPSession(server *RTMPServer, id uint64, ip string, c net.Conn) RTMPSession {
	return RTMPSession{
		server:        server,
		conn:          c,
		ip:            ip,
		mutex:         &sync.Mutex{},
		publish_mutex: &sync.Mutex{},
		id:            id,
		inChunkSize:   RTMP_CHUNK_SIZE,
		outChunkSize:  server.getOutChunkSize(),
		inPackets:     make(map[uint32]*RTMPPacket),
		ackSize:       0,
		inAckSize:     0,
		inLastAck:     0,

		bitRate: 0,
		bitRateCache: BitRateCache{
			intervalMs:  1000,
			last_update: 0,
			bytes:       0,
		},

		objectEncoding:  0,
		streams:         0,
		playStreamId:    0,
		publishStreamId: 0,

		receive_audio: true,
		receive_video: true,

		isConnected:  false,
		isPublishing: false,
		isPlaying:    false,
		isIdling:     false,
		isPause:      false,

		metaData:          make([]byte, 0),
		audioCodec:        0,
		videoCodec:        0,
		aacSequenceHeader: make([]byte, 0),
		avcSequenceHeader: make([]byte, 0),
		clock:             0,

		rtmpGopCache:     list.New(),
		gopCacheSize:     0,
		gopCacheLimit:    server.gopCacheLimit,
		gopCacheDisabled: false,
		gopPlayNo:        false,
		gopPlayClear:     false,

		channel:   "",
		key:       "",
		stream_id: "",
	}
}

// Sends data to the client
// b - The bytes to send
func (s *RTMPSession) SendSync(b []byte) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.conn.Write(b) //nolint:errcheck
}

// Closes the connection
func (s *RTMPSession) Kill() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.conn.Close()
}

// Returns the stream path: /{CHANNEL}/{KEY}
func (s *RTMPSession) GetStreamPath() string {
	return "/" + s.channel + "/" + s.key
}

// Handles the session
// Does the handshake and starts reading the chunks
func (s *RTMPSession) HandleSession() {
	r := bufio.NewReader(s.conn)

	e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
	if e != nil {
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
	e = s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
	if e != nil {
		LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
		return
	}
	n, e := io.ReadFull(r, handshakeBytes)
	if e != nil || n != RTMP_HANDSHAKE_SIZE {
		LogDebugSession(s.id, s.ip, "Invalid handshake received")
		return
	}

	s0s1s2 := generateS0S1S2(handshakeBytes)
	n, e = s.conn.Write(s0s1s2)
	if e != nil || n != len(s0s1s2) {
		LogDebugSession(s.id, s.ip, "Could not send handshake message")
		return
	}

	s1Copy := make([]byte, RTMP_HANDSHAKE_SIZE)
	e = s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
	if e != nil {
		LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
		return
	}
	n, e = io.ReadFull(r, s1Copy)
	if e != nil || n != RTMP_HANDSHAKE_SIZE {
		LogDebugSession(s.id, s.ip, "Invalid handshake response received")
		return
	}

	// Read RTMP chunks
	for {
		if !s.ReadChunk(r) {
			return
		}
	}
}

// Reads a chunk
// r - Buffered reader associated with the TCP connection
// Returns true if success, false if the connection is closed
func (s *RTMPSession) ReadChunk(r *bufio.Reader) bool {
	var bytesReadCount uint32
	bytesReadCount = 0

	// Start byte
	e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
	if e != nil {
		LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
		return false
	}
	startByte, e := r.ReadByte()
	bytesReadCount++
	if e != nil {
		LogDebugSession(s.id, s.ip, "Could not read chunk start byte. "+e.Error())
		return false
	}

	var header []byte
	header = []byte{startByte}

	var parserBasicBytes int
	if (startByte & 0x3f) == 0 {
		parserBasicBytes = 2
	} else if (startByte & 0x3f) == 1 {
		parserBasicBytes = 3
	} else {
		parserBasicBytes = 1
	}

	for i := 1; i < parserBasicBytes; i++ {
		e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
		if e != nil {
			LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
			return false
		}
		b, e := r.ReadByte()
		bytesReadCount++
		if e != nil {
			LogDebugSession(s.id, s.ip, "Could not read chunk basic bytes")
			return false
		}

		header = append(header, b)
	}

	// Header
	size := int(rtmpHeaderSize[header[0]>>6])
	if size > 0 {
		headerLeft := make([]byte, size)
		e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
		if e != nil {
			LogDebugSession(s.id, s.ip, "Could set deadline: "+e.Error())
			return false
		}
		n, e := io.ReadFull(r, headerLeft)
		bytesReadCount += uint32(size)
		if e != nil || n != size {
			LogDebugSession(s.id, s.ip, "Could not read chunk header")
			return false
		}
		header = append(header, headerLeft...)
	}

	// Parse packet
	var fmt uint32
	var cid uint32
	fmt = uint32(header[0] >> 6)
	switch parserBasicBytes {
	case 2:
		cid = 64 + uint32(header[1])
	case 3:
		cid = (64 + uint32(header[1]) + uint32(header[2])) << 8
	default:
		cid = uint32(header[0] & 0x3f)
	}

	var packet *RTMPPacket

	if s.inPackets[cid] != nil {
		packet = s.inPackets[cid]
		if packet.handled {
			packet.handled = false
			packet.payload = make([]byte, 0)
			packet.bytes = 0
		}
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
		tsBytes := make([]byte, 3)
		copy(tsBytes, header[offset:offset+3])
		packet.header.timestamp = int64((uint32(tsBytes[2])) | (uint32(tsBytes[1]) << 8) | (uint32(tsBytes[0]) << 16))
		offset += 3
	}

	// message length + type
	if packet.header.fmt <= RTMP_CHUNK_TYPE_1 {
		tsBytes := make([]byte, 3)
		copy(tsBytes, header[offset:offset+3])
		packet.header.length = (uint32(tsBytes[2])) | (uint32(tsBytes[1]) << 8) | (uint32(tsBytes[0]) << 16)
		packet.header.packet_type = uint32(header[offset+3])
		offset += 4
	}

	// Stream ID
	if packet.header.fmt == RTMP_CHUNK_TYPE_0 {
		packet.header.stream_id = binary.LittleEndian.Uint32(header[offset : offset+4])
		// offset += 4
	}

	if packet.header.packet_type > RTMP_TYPE_METADATA {
		LogDebugSession(s.id, s.ip, "Received stop packet: "+strconv.Itoa(int(packet.header.packet_type)))
		return false
	}

	// Extended timestamp
	var extended_timestamp int64
	if packet.header.timestamp == 0xffffff {
		tsBytes := make([]byte, 4)
		e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
		if e != nil {
			LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
			return false
		}
		n, e := io.ReadFull(r, tsBytes)
		bytesReadCount += 4
		if e != nil || n != 4 {
			LogDebugSession(s.id, s.ip, "Could not read extended timestamp")
			return false
		}
		extended_timestamp = int64(binary.BigEndian.Uint32(tsBytes))
	} else {
		extended_timestamp = packet.header.timestamp
	}

	if packet.bytes == 0 {
		if packet.header.fmt == RTMP_CHUNK_TYPE_0 {
			packet.clock = extended_timestamp
		} else {
			packet.clock += extended_timestamp
		}

		s.SetClock(packet.clock)

		if packet.capacity < packet.header.length {
			packet.capacity = 1024 + packet.header.length
		}
	}

	// Payload
	var sizeToRead uint32
	sizeToRead = s.inChunkSize - (packet.bytes % s.inChunkSize)
	if sizeToRead > (packet.header.length - packet.bytes) {
		sizeToRead = packet.header.length - packet.bytes
	}
	if sizeToRead > 0 {
		// LogDebugSession(s.id, s.ip, "Reading chunk with size: "+strconv.Itoa(int(sizeToRead))+" of total length = "+strconv.Itoa(int(packet.header.length)))
		bytesToRead := make([]byte, sizeToRead)
		e := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
		if e != nil {
			LogDebugSession(s.id, s.ip, "Could not set deadline: "+e.Error())
			return false
		}
		n, e := io.ReadFull(r, bytesToRead)
		bytesReadCount += sizeToRead
		if e != nil || uint32(n) != sizeToRead {
			if e != nil {
				LogDebugSession(s.id, s.ip, "Error: "+e.Error())
			}
			LogDebugSession(s.id, s.ip, "Could not read chunk payload")
			return false
		}

		packet.bytes += sizeToRead
		packet.payload = append(packet.payload, bytesToRead...)
	}

	// If packet is ready, handle
	if packet.bytes >= packet.header.length {
		packet.handled = true // Remove from pending packets
		if packet.clock <= 0xffffffff {
			if !s.HandlePacket(packet) {
				LogDebugSession(s.id, s.ip, "Could not handle packet")
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
			LogDebugSession(s.id, s.ip, "Could not send ACK")
			return false
		} else {
			LogDebugSession(s.id, s.ip, "Sent ACK: "+strconv.Itoa(int(s.inAckSize)))
		}
	}

	// Bitrate
	now := time.Now().UnixMilli()
	s.bitRateCache.bytes += uint64(bytesReadCount)
	diff := now - s.bitRateCache.last_update
	if diff >= s.bitRateCache.intervalMs {
		s.bitRate = uint64(math.Round(float64(s.bitRateCache.bytes) * 8 / float64(diff)))
		s.bitRateCache.bytes = 0
		s.bitRateCache.last_update = now
		LogDebugSession(s.id, s.ip, "Bitrate is now: "+strconv.Itoa(int(s.bitRate)))
	}

	return true
}

// Handles a packet
// packet - The received packet
func (s *RTMPSession) HandlePacket(packet *RTMPPacket) bool {
	switch packet.header.packet_type {
	case RTMP_TYPE_SET_CHUNK_SIZE:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_SET_CHUNK_SIZE")
		csb := packet.payload[0:4]
		s.inChunkSize = binary.BigEndian.Uint32(csb)
	case RTMP_TYPE_WINDOW_ACKNOWLEDGEMENT_SIZE:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_WINDOW_ACKNOWLEDGEMENT_SIZE")
		csb := packet.payload[0:4]
		s.ackSize = binary.BigEndian.Uint32(csb)
		LogDebugSession(s.id, s.ip, "ACK size updated: "+strconv.Itoa(int(s.ackSize)))
	case RTMP_TYPE_AUDIO:
		return s.HandleAudioPacket(packet)
	case RTMP_TYPE_VIDEO:
		return s.HandleVideoPacket(packet)
	case RTMP_TYPE_FLEX_MESSAGE:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_FLEX_MESSAGE")
		return s.HandleInvoke(packet)
	case RTMP_TYPE_INVOKE:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_INVOKE")
		return s.HandleInvoke(packet)
	case RTMP_TYPE_DATA:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_DATA")
		return s.HandleDataPacketAMF0(packet)
	case RTMP_TYPE_FLEX_STREAM:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_FLEX_STREAM")
		return s.HandleDataPacketAMF3(packet)
	default:
		LogDebugSession(s.id, s.ip, "Received packet: "+strconv.Itoa(int(packet.header.packet_type)))
	}

	return true
}

// Handles an INVOKE packet
// packet - The packet
func (s *RTMPSession) HandleInvoke(packet *RTMPPacket) bool {
	var offset uint32
	if packet.header.packet_type == RTMP_TYPE_FLEX_MESSAGE {
		offset = 1
	} else {
		offset = 0
	}

	payload := packet.payload[offset:packet.header.length]

	cmd := decodeRTMPCommand(payload)

	LogDebugSession(s.id, s.ip, "Received invoke: "+cmd.ToString())

	switch cmd.cmd {
	case "connect":
		return s.HandleConnect(&cmd)
	case "createStream":
		return s.HandleCreateStream(&cmd)
	case "publish":
		return s.HandlePublish(&cmd, packet)
	case "play":
		return s.HandlePlay(&cmd, packet)
	case "pause":
		return s.HandlePause(&cmd)
	case "deleteStream":
		return s.HandleDeleteStream(&cmd)
	case "closeStream":
		return s.HandleCloseStream(&cmd, packet)
	case "receiveAudio":
		s.receive_audio = cmd.GetArg("bool").GetBool()
	case "receiveVideo":
		s.receive_video = cmd.GetArg("bool").GetBool()
	}

	return true
}

// Handles a connect command
// cmd - The command
func (s *RTMPSession) HandleConnect(cmd *RTMPCommand) bool {
	s.channel = cmd.GetArg("cmdObj").GetProperty("app").GetString()

	// Validate channel
	if !validateStreamIDString(s.channel, s.server.streamIdMaxLength) {
		LogRequest(s.id, s.ip, "INVALID CHANNEL '"+s.channel+"'")
		return false
	}

	s.objectEncoding = uint32(cmd.GetArg("cmdObj").GetProperty("objectEncoding").GetInteger())
	s.connectTime = time.Now().UnixMilli()
	s.bitRateCache.intervalMs = 1000
	s.bitRateCache.last_update = s.connectTime
	s.bitRateCache.bytes = 0
	s.isConnected = true

	transId := cmd.GetArg("transId").GetInteger()

	LogRequest(s.id, s.ip, "CONNECT '"+s.channel+"'")

	s.SendWindowACK(5000000)
	s.SetPeerBandwidth(5000000, 2)
	s.SetChunkSize(s.outChunkSize)
	s.RespondConnect(transId, !cmd.GetArg("cmdObj").GetProperty("objectEncoding").IsUndefined())

	return true
}

// Handles a createStream command
// cmd - The command
func (s *RTMPSession) HandleCreateStream(cmd *RTMPCommand) bool {
	transId := cmd.GetArg("transId").GetInteger()
	s.RespondCreateStream(transId)

	return true
}

// Handles a publish command
// cmd - The command
// packet - The packet
func (s *RTMPSession) HandlePublish(cmd *RTMPCommand, packet *RTMPPacket) bool {
	sKeyPath := cmd.GetArg("streamName").GetString()
	sKeyPathSplit := strings.Split(sKeyPath, "?")
	s.key = sKeyPathSplit[0]

	if s.key == "" || !s.isConnected {
		return true
	}

	// Validate key
	if !validateStreamIDString(s.key, s.server.streamIdMaxLength) {
		s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadName", "Invalid stream key provided")
		return false
	}

	s.publishStreamId = packet.header.stream_id

	if s.isPublishing {
		s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadConnection", "Connection already publishing")
		return true
	}

	if s.server.isPublishing(s.channel) {
		s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadName", "Stream already publishing")
		return false
	}

	LogRequest(s.id, s.ip, "PUBLISH ("+strconv.Itoa(int(s.publishStreamId))+") '"+s.channel+"'")

	if s.server.websocketControlConnection != nil {
		// Coordinator
		pubAccepted, streamId := s.server.websocketControlConnection.RequestPublish(s.channel, s.key, s.ip)
		if !pubAccepted {
			LogRequest(s.id, s.ip, "Error: Invalid streaming key provided")
			s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadName", "Invalid stream key provided")
			return false
		}
		s.stream_id = streamId
	} else {
		// Callback
		if !s.SendStartCallback() {
			LogRequest(s.id, s.ip, "Error: Invalid streaming key provided")
			s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadName", "Invalid stream key provided")
			return false
		}
	}

	// Set publisher
	s.isPublishing = true
	s.server.SetPublisher(s.channel, s.key, s.stream_id, s)

	s.SendStatusMessage(s.publishStreamId, "status", "NetStream.Publish.Start", s.GetStreamPath()+" is now published.")

	s.StartIdlePlayers()

	return true
}

// Handles a play command
// cmd - The command
// packet - The packet
func (s *RTMPSession) HandlePlay(cmd *RTMPCommand, packet *RTMPPacket) bool {
	sKeyPath := cmd.GetArg("streamName").GetString()
	sKeyPathSplit := strings.Split(sKeyPath, "?")
	s.key = sKeyPathSplit[0]

	if len(sKeyPathSplit) > 1 {
		playParams := getRTMPParamsSimple(sKeyPathSplit[1])
		s.gopPlayNo = (playParams["cache"] == "no")
		s.gopPlayClear = (playParams["cache"] == "clear")
	}

	if s.key == "" || !s.isConnected {
		return true
	}

	s.playStreamId = packet.header.stream_id

	if s.isIdling || s.isPlaying {
		s.SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadConnection", "Connection already playing")
		return true
	}

	// Play whitelist
	if !s.CanPlay() {
		s.SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadName", "Your net address is not whitelisted for playing")
		return false
	}

	LogRequest(s.id, s.ip, "PLAY ("+strconv.Itoa(int(s.playStreamId))+") '"+s.channel+"'")

	s.RespondPlay()

	// Add player
	idle, e := s.server.AddPlayer(s.channel, s.key, s)

	if e != nil {
		LogRequest(s.id, s.ip, "Error: Invalid streaming key provided")
		s.SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadName", "Invalid stream key provided")
		return false // Invalid key
	}

	if !idle {
		publisher := s.server.GetPublisher(s.channel)
		if publisher != nil {
			publisher.StartPlayer(s)
		}
	} else {
		LogRequest(s.id, s.ip, "PLAY IDLE '"+s.channel+"'")
	}

	return true
}

// Handles a pause command
// cmd - The command
func (s *RTMPSession) HandlePause(cmd *RTMPCommand) bool {
	if !s.isPlaying {
		return true
	}

	s.isPause = cmd.GetArg("pause").GetBool()

	if s.isPause {
		s.SendStreamStatus(STREAM_EOF, s.playStreamId)
		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Pause.Notify", "Paused live")
		LogRequest(s.id, s.ip, "PAUSE '"+s.channel+"'")
	} else {
		s.SendStreamStatus(STREAM_BEGIN, s.playStreamId)
		publisher := s.server.GetPublisher(s.channel)

		if publisher != nil {
			LogRequest(s.id, s.ip, "RESUME '"+s.channel+"'")
			publisher.ResumePlayer(s)
		} else {
			LogRequest(s.id, s.ip, "PLAY IDLE '"+s.channel+"'")
		}

		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Unpause.Notify", "Unpaused live")
	}

	return true
}

// Handles a deleteStream command
// cmd - The command
func (s *RTMPSession) HandleDeleteStream(cmd *RTMPCommand) bool {
	streamId := uint32(cmd.GetArg("streamId").GetInteger())

	if streamId == s.playStreamId {
		// Close play
		LogRequest(s.id, s.ip, "PLAY STOP '"+s.channel+"'")

		s.server.RemovePlayer(s.channel, s.key, s)

		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Play.Stop", "Stopped playing stream.")

		s.playStreamId = 0
		s.isPlaying = false
		s.isIdling = false
	}

	if streamId == s.publishStreamId {
		// Close publish
		LogDebugSession(s.id, s.ip, "Close publish stream")

		if s.isPublishing {
			s.EndPublish(false)
		}

		s.publishStreamId = 0
	}

	return true
}

// Deletes a stream
// streamId - ID of the stream
func (s *RTMPSession) DeleteStream(streamId uint32) {
	if streamId == s.playStreamId {
		// Close play
		LogDebugSession(s.id, s.ip, "Close play stream: "+strconv.Itoa(int(streamId)))

		s.server.RemovePlayer(s.channel, s.key, s)

		s.playStreamId = 0
		s.isPlaying = false
		s.isIdling = false
	}

	if streamId == s.publishStreamId {
		// Close publish
		LogDebugSession(s.id, s.ip, "Close publish stream: "+strconv.Itoa(int(streamId)))

		if s.isPublishing {
			s.EndPublish(true)
		}

		s.publishStreamId = 0
	}
}

// Handles a closeStream command
// cmd - The command
// packet - The packet
func (s *RTMPSession) HandleCloseStream(cmd *RTMPCommand, packet *RTMPPacket) bool {
	streamId := createAMF0Value(AMF0_TYPE_NUMBER)
	streamId.SetIntegerVal(int64(packet.header.stream_id))
	cmd.arguments["streamId"] = &streamId
	return s.HandleDeleteStream(cmd)
}

// Handles an audio packet  (contains audio data)
// packet - The packet
func (s *RTMPSession) HandleAudioPacket(packet *RTMPPacket) bool {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if !s.isPublishing {
		return true
	}

	sound_format := (packet.payload[0] >> 4) & 0x0f

	if s.audioCodec == 0 {
		s.audioCodec = uint32(sound_format)
	}

	isHeader := (sound_format == 10 || sound_format == 13) && packet.payload[1] == 0

	if isHeader {
		s.aacSequenceHeader = packet.payload
	}

	cachePacket := createBlankRTMPPacket()
	cachePacket.header.fmt = RTMP_CHUNK_TYPE_0
	cachePacket.header.cid = RTMP_CHANNEL_AUDIO
	cachePacket.header.packet_type = RTMP_TYPE_AUDIO
	cachePacket.payload = packet.payload
	cachePacket.header.length = uint32(len(cachePacket.payload))
	cachePacket.header.timestamp = s.clock

	if !isHeader && !s.gopCacheDisabled {
		s.rtmpGopCache.PushBack(&cachePacket)
		s.gopCacheSize += int64(cachePacket.header.length) + RTMP_PACKET_BASE_SIZE

		for s.gopCacheSize > s.gopCacheLimit {
			toDelete := s.rtmpGopCache.Front()
			v := toDelete.Value
			switch x := v.(type) {
			case *RTMPPacket:
				s.gopCacheSize -= int64(x.header.length)
			}
			s.rtmpGopCache.Remove(toDelete)
			s.gopCacheSize -= RTMP_PACKET_BASE_SIZE
		}
	}

	players := s.server.GetPlayers(s.channel)

	for i := 0; i < len(players); i++ {
		if players[i].isPlaying && !players[i].isPause && players[i].receive_audio {
			players[i].SendCachePacket(&cachePacket)
		}
	}

	return true
}

// Handles a video packet (Contains video data)
// packet - The packet
func (s *RTMPSession) HandleVideoPacket(packet *RTMPPacket) bool {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if !s.isPublishing {
		return true
	}

	frame_type := (packet.payload[0] >> 4) & 0x0f
	codec_id := packet.payload[0] & 0x0f

	isHeader := (codec_id == 7 || codec_id == 12) && (frame_type == 1 && packet.payload[1] == 0)

	if isHeader {
		s.avcSequenceHeader = packet.payload
		s.rtmpGopCache = list.New()
		s.gopCacheSize = 0
	}

	if s.videoCodec == 0 {
		s.videoCodec = uint32(codec_id)
	}

	cachePacket := createBlankRTMPPacket()
	cachePacket.header.fmt = RTMP_CHUNK_TYPE_0
	cachePacket.header.cid = RTMP_CHANNEL_VIDEO
	cachePacket.header.packet_type = RTMP_TYPE_VIDEO
	cachePacket.payload = packet.payload
	cachePacket.header.length = uint32(len(cachePacket.payload))
	cachePacket.header.timestamp = s.clock

	// Cache
	if !isHeader && !s.gopCacheDisabled {
		s.rtmpGopCache.PushBack(&cachePacket)
		s.gopCacheSize += int64(cachePacket.header.length) + RTMP_PACKET_BASE_SIZE

		for s.gopCacheSize > s.gopCacheLimit {
			toDelete := s.rtmpGopCache.Front()
			v := toDelete.Value
			switch x := v.(type) {
			case *RTMPPacket:
				s.gopCacheSize -= int64(x.header.length)
			}
			s.rtmpGopCache.Remove(toDelete)
			s.gopCacheSize -= RTMP_PACKET_BASE_SIZE
		}
	}

	players := s.server.GetPlayers(s.channel)

	for i := 0; i < len(players); i++ {
		if players[i].isPlaying && !players[i].isPause && players[i].receive_video {
			players[i].SendCachePacket(&cachePacket)
		}
	}

	return true
}

// Handles a data packet encoded with AMF0
// packet the packet
func (s *RTMPSession) HandleDataPacketAMF0(packet *RTMPPacket) bool {
	data := decodeRTMPData(packet.payload)
	return s.HandleRTMPData(packet, &data)
}

// Handles a data packet encoded with AMF3
// packet the packet
func (s *RTMPSession) HandleDataPacketAMF3(packet *RTMPPacket) bool {
	data := decodeRTMPData(packet.payload[1:])
	return s.HandleRTMPData(packet, &data)
}

// Handles a data packet
// packet - The packet
// data - The decoded data message
func (s *RTMPSession) HandleRTMPData(packet *RTMPPacket, data *RTMPData) bool {
	LogDebugSession(s.id, s.ip, "Received data: "+data.ToString())
	switch data.tag {
	case "@setDataFrame":
		metaData := s.BuildMetadata(data)
		s.SetMetaData(metaData)
	}

	return true
}

// Call after the TCP connection is closed
func (s *RTMPSession) OnClose() {
	if s.playStreamId > 0 {
		s.DeleteStream(s.playStreamId)
	}
	if s.publishStreamId > 0 {
		s.DeleteStream(s.publishStreamId)
	}

	s.isConnected = false
}
