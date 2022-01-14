// RTMP server

package main

import (
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/netdata/go.d.plugin/pkg/iprange"
)

type RTMPChannel struct {
	channel       string
	key           string
	stream_id     string
	publisher     uint64
	is_publishing bool
	players       map[uint64]bool
}

type RTMPServer struct {
	port            int
	listener        net.Listener
	secureListener  net.Listener
	mutex           *sync.Mutex
	sessions        map[uint64]*RTMPSession
	channels        map[string]*RTMPChannel
	ip_count        map[string]uint32
	ip_limit        uint32
	ip_mutex        *sync.Mutex
	next_session_id uint64
	gopCacheLimit   int64
	closed          bool
}

func CreateRTMPServer() *RTMPServer {
	server := RTMPServer{
		listener:        nil,
		secureListener:  nil,
		mutex:           &sync.Mutex{},
		ip_mutex:        &sync.Mutex{},
		sessions:        make(map[uint64]*RTMPSession),
		channels:        make(map[string]*RTMPChannel),
		next_session_id: 1,
		closed:          false,
		ip_count:        make(map[string]uint32),
		ip_limit:        4,
		gopCacheLimit:   256 * 1024 * 1024,
	}

	custom_ip_limit := os.Getenv("MAX_IP_CONCURRENT_CONNECTIONS")
	if custom_ip_limit != "" {
		cil, e := strconv.Atoi(custom_ip_limit)
		if e != nil {
			server.ip_limit = uint32(cil)
		}
	}

	custom_gop_limit := os.Getenv("GOP_CACHE_SIZE_MB")
	if custom_gop_limit != "" {
		cgl, e := strconv.Atoi(custom_gop_limit)
		if e != nil {
			server.gopCacheLimit = int64(cgl) * 1024 * 1024
		}
	}

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup RTMP server
	var tcp_port int
	tcp_port = 1935
	customTCPPort := os.Getenv("RTMP_PORT")
	if customTCPPort != "" {
		tcpp, e := strconv.Atoi(customTCPPort)
		if e == nil {
			tcp_port = tcpp
		}
	}
	server.port = tcp_port

	lTCP, errTCP := net.Listen("tcp", bind_addr+":"+strconv.Itoa(tcp_port))
	if errTCP != nil {
		LogError(errTCP)
		return nil
	} else {
		server.listener = lTCP
		LogInfo("[RTMP] Listing on " + bind_addr + ":" + strconv.Itoa(tcp_port))
	}

	// Setup RTMPS server
	var ssl_port int
	ssl_port = 443
	customSSLPort := os.Getenv("SSL_PORT")
	if customSSLPort != "" {
		sslp, e := strconv.Atoi(customSSLPort)
		if e == nil {
			ssl_port = sslp
		}
	}

	certFile := os.Getenv("SSL_CERT")
	keyFile := os.Getenv("SSL_KEY")

	if certFile != "" && keyFile != "" {
		cer, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			LogError(err)
			if server.listener != nil {
				server.listener.Close()
			}
			return nil
		}

		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		lnSSL, errSSL := tls.Listen("tcp", bind_addr+":"+strconv.Itoa(ssl_port), config)
		if errSSL != nil {
			LogError(errSSL)
			return nil
		} else {
			server.secureListener = lnSSL
			LogInfo("[SSL] Listening on " + bind_addr + ":" + strconv.Itoa(ssl_port))
		}
	}

	return &server
}

func (server *RTMPServer) AddIP(ip string) bool {
	server.ip_mutex.Lock()
	defer server.ip_mutex.Unlock()

	c := server.ip_count[ip]

	if c >= server.ip_limit {
		return false
	}

	server.ip_count[ip] = c + 1

	return true
}

func (server *RTMPServer) isIPExempted(ipStr string) bool {
	r := os.Getenv("CONCURRENT_LIMIT_WHITELIST")

	if r == "" {
		return false
	}

	if r == "*" {
		return true
	}

	ip := net.ParseIP(ipStr)

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

func (server *RTMPServer) RemoveIP(ip string) {
	server.ip_mutex.Lock()
	defer server.ip_mutex.Unlock()

	c := server.ip_count[ip]

	if c <= 1 {
		delete(server.ip_count, ip)
	} else {
		server.ip_count[ip] = c - 1
	}
}

func (server *RTMPServer) NextSessionID() uint64 {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	r := server.next_session_id
	server.next_session_id++
	return r
}

func (server *RTMPServer) AddSession(s *RTMPSession) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	server.sessions[s.id] = s
}

func (server *RTMPServer) RemoveSession(id uint64) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	delete(server.sessions, id)
}

func (server *RTMPServer) isPublishing(channel string) bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	return server.channels[channel] != nil && server.channels[channel].is_publishing
}

func (server *RTMPServer) GetPublisher(channel string) *RTMPSession {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] == nil {
		return nil
	}

	if !server.channels[channel].is_publishing {
		return nil
	}

	id := server.channels[channel].publisher
	return server.sessions[id]
}

func (server *RTMPServer) SetPublisher(channel string, key string, stream_id string, s *RTMPSession) bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] != nil && server.channels[channel].is_publishing {
		return false
	}

	if server.channels[channel] == nil {
		c := RTMPChannel{
			channel:       channel,
			key:           key,
			stream_id:     stream_id,
			is_publishing: true,
			publisher:     s.id,
			players:       make(map[uint64]bool),
		}
		server.channels[channel] = &c
	} else {
		server.channels[channel].key = key
		server.channels[channel].stream_id = stream_id
		server.channels[channel].is_publishing = true
		server.channels[channel].publisher = s.id
	}

	return true
}

func (server *RTMPServer) RemovePublisher(channel string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] == nil {
		return
	}

	server.channels[channel].publisher = 0
	server.channels[channel].is_publishing = false

	players := server.channels[channel].players

	for sid := range players {
		player := server.sessions[sid]
		if player != nil {
			player.isIdling = true
			player.isPlaying = false
		}
	}

	if !server.channels[channel].is_publishing && len(server.channels[channel].players) == 0 {
		delete(server.channels, channel)
	}
}

func (server *RTMPServer) GetIdlePlayers(channel string) []*RTMPSession {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] == nil {
		return make([]*RTMPSession, 0)
	}

	players := server.channels[channel].players

	playersToStart := make([]*RTMPSession, 0)

	for sid := range players {
		player := server.sessions[sid]
		if player != nil && player.isIdling {
			playersToStart = append(playersToStart, player)
		}
	}

	return playersToStart
}

func (server *RTMPServer) GetPlayers(channel string) []*RTMPSession {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] == nil {
		return make([]*RTMPSession, 0)
	}

	players := server.channels[channel].players

	playersToStart := make([]*RTMPSession, 0)

	for sid := range players {
		player := server.sessions[sid]
		if player != nil && player.isPlaying {
			playersToStart = append(playersToStart, player)
		}
	}

	return playersToStart
}

func (server *RTMPServer) AddPlayer(channel string, key string, s *RTMPSession) (bool, error) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	if server.channels[channel] == nil {
		c := RTMPChannel{
			channel:       channel,
			key:           key,
			stream_id:     "",
			is_publishing: false,
			publisher:     0,
			players:       make(map[uint64]bool),
		}
		server.channels[channel] = &c
	}

	if server.channels[channel].is_publishing {
		if subtle.ConstantTimeCompare([]byte(key), []byte(server.channels[channel].key)) == 1 {
			s.isIdling = false
		} else {
			return false, errors.New("Invalid key")
		}
	} else {
		s.isIdling = true
	}

	server.channels[channel].players[s.id] = true

	return s.isIdling, nil
}

func (server *RTMPServer) RemovePlayer(channel string, key string, s *RTMPSession) {
	if server.channels[channel] == nil {
		return
	}

	delete(server.channels[channel].players, s.id)

	s.isIdling = false
	s.isPlaying = false

	if !server.channels[channel].is_publishing && len(server.channels[channel].players) == 0 {
		delete(server.channels, channel)
	}
}

func (server *RTMPServer) AcceptConnections(listener net.Listener, wg *sync.WaitGroup) {
	defer func() {
		listener.Close()
		wg.Done()
	}()
	for {
		c, err := listener.Accept()
		if err != nil {
			LogError(err)
			return
		}
		id := server.NextSessionID()
		var ip string
		if addr, ok := c.RemoteAddr().(*net.TCPAddr); ok {
			ip = addr.IP.String()
		} else {
			ip = c.RemoteAddr().String()
		}

		if !server.isIPExempted(ip) {
			if !server.AddIP(ip) {
				c.Close()
				LogRequest(id, ip, "Connection rejected: Too many requests")
				continue
			}
		}

		LogDebugSession(id, ip, "Connection accepted!")
		go server.HandleConnection(id, ip, c)
	}
}

func (server *RTMPServer) SendPings(wg *sync.WaitGroup) {
	defer wg.Done()
	for !server.closed {
		// Wait
		time.Sleep(RTMP_PING_TIME * time.Millisecond)

		func() {
			server.mutex.Lock()
			defer server.mutex.Unlock()

			for _, s := range server.sessions {
				s.SendPingRequest()
			}
		}()
	}
}

func (server *RTMPServer) Start() {
	var wg sync.WaitGroup
	if server.listener != nil {
		wg.Add(1)
		go server.AcceptConnections(server.listener, &wg)
	}

	if server.secureListener != nil {
		wg.Add(1)
		go server.AcceptConnections(server.secureListener, &wg)
	}

	wg.Add(1)
	go server.SendPings(&wg)

	wg.Wait()
}

func (server *RTMPServer) HandleConnection(id uint64, ip string, c net.Conn) {
	s := CreateRTMPSession(server, id, ip, c)

	server.AddSession(&s)

	defer func() {
		if err := recover(); err != nil {
			switch x := err.(type) {
			case string:
				LogRequest(id, ip, "Error: "+x)
			case error:
				LogRequest(id, ip, "Error: "+x.Error())
			default:
				LogRequest(id, ip, "Connection Crashed!")
			}
		}
		s.OnClose()
		c.Close()
		server.RemoveSession(id)
		server.RemoveIP(ip)
		LogDebugSession(id, ip, "Connection closed!")
	}()

	s.HandleSession()
}

func (server *RTMPServer) getOutChunkSize() uint32 {
	r := os.Getenv("RTMP_CHUNK_SIZE")

	if r == "" {
		return RTMP_CHUNK_SIZE
	}

	n, e := strconv.Atoi(r)

	if e != nil || n <= RTMP_CHUNK_SIZE {
		return RTMP_CHUNK_SIZE
	}

	return uint32(n)
}
