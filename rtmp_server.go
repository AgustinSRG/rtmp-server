// RTMP server

package main

import (
	"crypto/tls"
	"net"
	"os"
	"strconv"
	"sync"
)

type RTMPPathStatus struct {
	channel       string
	key           string
	stream_id     string
	publisher     uint64
	is_publishing bool
	players       map[uint64]bool
}

type RTMPServer struct {
	listener        net.Listener
	secureListener  net.Listener
	mutex           *sync.Mutex
	sessions        map[uint64]RTMPSession
	paths           map[string]RTMPPathStatus
	next_session_id uint64
}

func CreateRTMPServer() *RTMPServer {
	server := RTMPServer{
		listener:        nil,
		secureListener:  nil,
		mutex:           &sync.Mutex{},
		sessions:        make(map[uint64]RTMPSession),
		paths:           make(map[string]RTMPPathStatus),
		next_session_id: 1,
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

func (server *RTMPServer) NextSessionID() uint64 {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	r := server.next_session_id
	server.next_session_id++
	return r
}

func (server *RTMPServer) AddSession(s RTMPSession) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	server.sessions[s.id] = s
}

func (server *RTMPServer) RemoveSession(id uint64) {
	server.mutex.Lock()
	defer server.mutex.Unlock()

	delete(server.sessions, id)
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
		LogRequest(id, ip, "Connection accepted!")
		go server.HandleConnection(id, ip, c)
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

	wg.Wait()
}

func (server *RTMPServer) HandleConnection(id uint64, ip string, c net.Conn) {
	s := CreateRTMPSession(server, id, ip, c)

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
		LogRequest(id, ip, "Connection closed!")
	}()

	s.HandleSession()
}
