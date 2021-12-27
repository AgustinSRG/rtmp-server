// RTMP session

package main

import (
	"bufio"
	"net"
	"sync"
)

type RTMPSession struct {
	server *RTMPServer
	conn   net.Conn
	ip     string
	mutex  *sync.Mutex

	id uint64
}

func CreateRTMPSession(server *RTMPServer, id uint64, ip string, c net.Conn) RTMPSession {
	return RTMPSession{
		server: server,
		conn:   c,
		ip:     ip,
		mutex:  &sync.Mutex{},
		id:     id,
	}
}

func (s *RTMPSession) HandleSession() {
	r := bufio.NewReader(s.conn)

	for {
		msg, err := r.ReadString('\n')
		if err != nil {
			return
		}

		LogRequest(s.id, s.ip, "RCV: "+msg)
	}
}

func (s *RTMPSession) OnClose() {
}
