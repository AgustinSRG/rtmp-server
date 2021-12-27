// RTMP session

package main

import (
	"bufio"
	"net"
	"sync"
	"time"
)

type RTMPSession struct {
	server *RTMPServer
	conn   net.Conn
	ip     string
	mutex  *sync.Mutex

	id uint64

	handshakeState uint32
}

func CreateRTMPSession(server *RTMPServer, id uint64, ip string, c net.Conn) RTMPSession {
	return RTMPSession{
		server:         server,
		conn:           c,
		ip:             ip,
		mutex:          &sync.Mutex{},
		id:             id,
		handshakeState: RTMP_HANDSHAKE_UNINIT,
	}
}

func (s *RTMPSession) HandleSession() {
	r := bufio.NewReader(s.conn)

	for {
		err := s.conn.SetReadDeadline(time.Now().Add(RTMP_PING_TIMEOUT * time.Millisecond))
		if err != nil {
			return
		}

		msg, err := r.ReadString('\n')
		if err != nil {
			return
		}

		LogRequest(s.id, s.ip, "RCV: "+msg)
	}
}

func (s *RTMPSession) OnClose() {
}
