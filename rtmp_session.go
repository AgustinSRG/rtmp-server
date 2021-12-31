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

type BitrateCache struct {
	intervalMs  int64
	last_update int64
	bytes       uint64
}

type RTMPSession struct {
	server        *RTMPServer
	conn          net.Conn
	ip            string
	mutex         *sync.Mutex
	publish_mutex *sync.Mutex

	id          uint64
	inChunkSize uint32

	inPackets map[uint32]*RTMPPacket

	ackSize   uint32
	inAckSize uint32
	inLastAck uint32

	objectEncoding  uint32
	connectTime     int64
	playStreamId    uint32
	publishStreamId uint32

	receive_audio bool
	receive_video bool

	channel   string
	key       string
	stream_id string

	isConnected  bool
	isPublishing bool
	isPlaying    bool
	isIdling     bool
	isPause      bool

	metaData          []byte
	audioCodec        uint32
	videoCodec        uint32
	aacSequenceHeader []byte
	avcSequenceHeader []byte
	clock             int64

	rtmpGopcache *list.List

	streams uint32

	bitrate       uint64
	bitrate_cache BitrateCache
}

func CreateRTMPSession(server *RTMPServer, id uint64, ip string, c net.Conn) RTMPSession {
	return RTMPSession{
		server:        server,
		conn:          c,
		ip:            ip,
		mutex:         &sync.Mutex{},
		publish_mutex: &sync.Mutex{},
		id:            id,
		inChunkSize:   RTMP_CHUNK_SIZE,
		inPackets:     make(map[uint32]*RTMPPacket),
		ackSize:       0,
		inAckSize:     0,
		inLastAck:     0,

		bitrate: 0,
		bitrate_cache: BitrateCache{
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

		rtmpGopcache: list.New(),

		channel:   "",
		key:       "",
		stream_id: "",
	}
}

func (s *RTMPSession) SendSync(b []byte) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.conn.Write(b)
}

func (s *RTMPSession) Kill() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.conn.Close()
}

func (s *RTMPSession) GetStreamPath() string {
	return "/" + s.channel + "/" + s.key
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
	n, e = io.ReadFull(r, s1Copy)
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
		LogDebugSession(s.id, s.ip, "Could not read chunk start byte")
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
			LogDebugSession(s.id, s.ip, "Could not read chunk basic bytes")
			return false
		}

		header = append(header, b)
	}

	// Header
	size := int(rtmpHeaderSize[header[0]>>6])
	if size > 0 {
		headerLeft := make([]byte, size)
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

	if s.inPackets[cid] != nil {
		packet = s.inPackets[cid]
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
	if packet.header.fmt <= RTMP_CHUNK_TYPE_0 {
		packet.header.stream_id = binary.LittleEndian.Uint32(header[offset : offset+4])
		offset += 4
	}

	if packet.header.packet_type > RTMP_TYPE_METADATA {
		return false
	}

	// Extended typestamp
	var extended_timestamp int64
	if packet.header.timestamp == 0xffffff {
		tsBytes := make([]byte, 4)
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
		LogDebugSession(s.id, s.ip, "Reading chunk with size: "+strconv.Itoa(int(sizeToRead))+" of total length = "+strconv.Itoa(int(packet.header.length)))
		bytesToRead := make([]byte, sizeToRead)
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
		delete(s.inPackets, packet.header.cid) // Remove from pending packets
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
			LogDebugSession(s.id, s.ip, "Could not send ACK")
			return false
		} else {
			LogDebugSession(s.id, s.ip, "Sent ACK")
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
		LogDebugSession(s.id, s.ip, "Bitrate is now: "+strconv.Itoa(int(s.bitrate)))
	}

	return true
}

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
	case RTMP_TYPE_AUDIO:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_AUDIO")
		return s.HandleAudioPacket(packet)
	case RTMP_TYPE_VIDEO:
		LogDebugSession(s.id, s.ip, "Received packet: RTMP_TYPE_VIDEO")
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

func (s *RTMPSession) HandleInvoke(packet *RTMPPacket) bool {
	var offset uint32
	if packet.header.packet_type == RTMP_TYPE_FLEX_MESSAGE {
		offset = 1
	} else {
		offset = 0
	}

	payload := packet.payload[offset:packet.header.length]

	cmd := decodeRTMPCommand(payload)

	LogDebugSession(s.id, s.ip, "Received invoke: "+cmd.cmd)

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
		s.receive_audio = cmd.arguments["bool"].GetBool()
	case "receiveVideo":
		s.receive_video = cmd.arguments["bool"].GetBool()
	}

	return true
}

func (s *RTMPSession) HandleConnect(cmd *RTMPCommand) bool {
	s.channel = cmd.arguments["cmdObj"].GetObject()["app"].GetString()

	// Validate channel
	if validateStreamIDString(s.channel) {
		return false
	}

	s.objectEncoding = uint32(cmd.arguments["cmdObj"].GetObject()["objectEncoding"].GetInteger())
	s.connectTime = time.Now().UnixMilli()
	s.bitrate_cache.intervalMs = 1000
	s.bitrate_cache.last_update = s.connectTime
	s.bitrate_cache.bytes = 0
	s.isConnected = true

	transId := cmd.arguments["transId"].GetInteger()

	s.SendWindowACK(5000000)
	s.SetPeerBandwidth(5000000, 2)
	s.SetChunkSize(RTMP_CHUNK_SIZE)
	s.RespondConnect(transId)

	return true
}

func (s *RTMPSession) HandleCreateStream(cmd *RTMPCommand) bool {
	transId := cmd.arguments["transId"].GetInteger()
	s.RespondCreateStream(transId)

	return true
}

func (s *RTMPSession) HandlePublish(cmd *RTMPCommand, packet *RTMPPacket) bool {
	sKeyPath := cmd.arguments["streamName"].GetString()
	sKeyPathSplit := strings.Split(sKeyPath, "?")
	s.key = sKeyPathSplit[0]

	if s.key == "" || !s.isConnected {
		return true
	}

	// Validate key
	if !validateStreamIDString(s.key) {
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

	// Callback limit (TODO)

	// Callback
	if !s.SendStartCallback() {
		s.SendStatusMessage(s.publishStreamId, "error", "NetStream.Publish.BadName", "Invalid stream key provided")
		return false
	}

	// Set publisher
	s.isPublishing = true
	s.server.SetPublisher(s.channel, s.key, s.stream_id, s)

	s.SendStatusMessage(s.publishStreamId, "status", "NetStream.Publish.Start", s.GetStreamPath()+" is now published.")

	s.StartIdlePlayers()

	return true
}

func (s *RTMPSession) HandlePlay(cmd *RTMPCommand, packet *RTMPPacket) bool {
	sKeyPath := cmd.arguments["streamName"].GetString()
	sKeyPathSplit := strings.Split(sKeyPath, "?")
	s.key = sKeyPathSplit[0]

	if s.key == "" || !s.isConnected {
		return true
	}

	s.playStreamId = packet.header.stream_id

	if s.isIdling || s.isPlaying {
		s.SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadConnection", "Connection already playing")
		return true
	}

	// TODO: Play whitelist

	s.RespondPlay()

	// Add player
	idle, e := s.server.AddPlayer(s.channel, s.key, s)

	if e != nil {
		s.SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadName", "Invalid stream key provided")
		return false // Invalid key
	}

	if !idle {
		publisher := s.server.GetPublisher(s.channel)
		if publisher != nil {
			publisher.StartPlayer(s)
		}
	}

	return true
}

func (s *RTMPSession) HandlePause(cmd *RTMPCommand) bool {
	if !s.isPlaying {
		return true
	}

	s.isPause = cmd.arguments["pause"].GetBool()

	if s.isPause {
		s.SendStreamStatus(STREAM_EOF, s.playStreamId)
		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Pause.Notify", "Paused live")
	} else {
		s.SendStreamStatus(STREAM_BEGIN, s.playStreamId)
		publisher := s.server.GetPublisher(s.channel)

		if publisher != nil {
			publisher.ResumePlayer(s)
		}

		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Unpause.Notify", "Unpaused live")
	}

	return true
}

func (s *RTMPSession) HandleDeleteStream(cmd *RTMPCommand) bool {
	streamId := uint32(cmd.arguments["streamId"].GetInteger())

	if streamId == s.playStreamId {
		// Close play

		s.server.RemovePlayer(s.channel, s.key, s)

		s.SendStatusMessage(s.playStreamId, "status", "NetStream.Play.Stop", "Stopped playing stream.")

		s.playStreamId = 0
		s.isPlaying = false
		s.isIdling = false
	}

	if streamId == s.publishStreamId {
		// Close publish

		if s.isPublishing {
			s.EndPublish(false)
		}

		s.publishStreamId = 0
	}

	return true
}

func (s *RTMPSession) DeleteStream(streamId uint32) {

	if streamId == s.playStreamId {
		// Close play

		s.server.RemovePlayer(s.channel, s.key, s)

		s.playStreamId = 0
		s.isPlaying = false
		s.isIdling = false
	}

	if streamId == s.publishStreamId {
		// Close publish

		if s.isPublishing {
			s.EndPublish(true)
		}

		s.publishStreamId = 0
	}
}

func (s *RTMPSession) HandleCloseStream(cmd *RTMPCommand, packet *RTMPPacket) bool {
	streamId := createAMF0Value(AMF0_TYPE_NUMBER)
	streamId.SetIntegerVal(int64(packet.header.stream_id))
	cmd.arguments["streamId"] = &streamId
	return s.HandleDeleteStream(cmd)
}

func (s *RTMPSession) OnClose() {
	if s.playStreamId > 0 {
		s.DeleteStream(s.playStreamId)
	}

	if s.publishStreamId > 0 {
		s.DeleteStream(s.publishStreamId)
	}

	s.isConnected = false
}

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

	if (sound_format == 10 || sound_format == 13) && packet.payload[1] == 0 {
		s.aacSequenceHeader = packet.payload
	}

	cachePacket := createBlankRTMPPacket()
	cachePacket.header.fmt = RTMP_CHUNK_TYPE_0
	cachePacket.header.cid = RTMP_CHANNEL_AUDIO
	cachePacket.header.packet_type = RTMP_TYPE_AUDIO
	cachePacket.payload = packet.payload
	cachePacket.header.length = uint32(len(cachePacket.payload))
	cachePacket.header.timestamp = s.clock

	if len(s.aacSequenceHeader) == 0 || packet.payload[1] != 0 {
		s.rtmpGopcache.PushBack(&cachePacket)

		if s.rtmpGopcache.Len() > RTMP_GOP_CACHE_SIZE {
			s.rtmpGopcache.Remove(s.rtmpGopcache.Front())
		}
	}

	players := s.server.GetPlayers(s.channel)

	for i := 0; i < len(players); i++ {
		if players[i].isPlaying && !players[i].isPause && !players[i].receive_audio {
			players[i].SendCachePacket(&cachePacket)
		}
	}

	return true
}

func (s *RTMPSession) HandleVideoPacket(packet *RTMPPacket) bool {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if !s.isPublishing {
		return true
	}

	frame_type := (packet.payload[0] >> 4) & 0x0f
	codec_id := packet.payload[0] & 0x0f

	if codec_id == 7 || codec_id == 12 {
		if frame_type == 1 && packet.payload[1] == 0 {
			s.avcSequenceHeader = packet.payload
			s.rtmpGopcache = list.New()
		}
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
	if codec_id == 7 || codec_id == 12 {
		if frame_type != 1 || packet.payload[1] != 0 {
			s.rtmpGopcache.PushBack(&cachePacket)

			if s.rtmpGopcache.Len() > RTMP_GOP_CACHE_SIZE {
				s.rtmpGopcache.Remove(s.rtmpGopcache.Front())
			}
		}
	}

	players := s.server.GetPlayers(s.channel)

	for i := 0; i < len(players); i++ {
		if players[i].isPlaying && !players[i].isPause && !players[i].receive_audio {
			players[i].SendCachePacket(&cachePacket)
		}
	}

	return true
}

func (s *RTMPSession) HandleDataPacketAMF0(packet *RTMPPacket) bool {
	data := decodeRTMPData(packet.payload)
	return s.HandleRTMPData(packet, &data)
}

func (s *RTMPSession) HandleDataPacketAMF3(packet *RTMPPacket) bool {
	data := decodeRTMPData(packet.payload[1:])
	return s.HandleRTMPData(packet, &data)
}

func (s *RTMPSession) HandleRTMPData(packet *RTMPPacket, data *RTMPData) bool {
	switch data.tag {
	case "@setDataFrame":
		metaData := s.BuildMetadata(data)
		s.SetMetaData(metaData)
	}

	return true
}
