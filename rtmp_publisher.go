// RTMP session publisher methods

package main

import (
	"container/list"
	"crypto/subtle"
)

// Starts sending to idle players
// Call only for publishers
func (s *RTMPSession) StartIdlePlayers() {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	// Start idle players
	idlePlayers := s.server.GetIdlePlayers(s.channel)

	for i := 0; i < len(idlePlayers); i++ {
		if subtle.ConstantTimeCompare([]byte(s.key), []byte(idlePlayers[i].key)) == 1 {
			player := idlePlayers[i]

			LogRequest(player.id, player.ip, "PLAY START '"+player.channel+"'")

			player.SendMetadata(s.metaData, 0)
			player.SendAudioCodecHeader(s.audioCodec, s.aacSequenceHeader, 0)
			player.SendVideoCodecHeader(s.videoCodec, s.avcSequenceHeader, 0)

			if !player.gopPlayNo && s.rtmpGopCache.Len() > 0 {
				for t := s.rtmpGopCache.Front(); t != nil; t = t.Next() {
					chunks := t.Value
					switch x := chunks.(type) {
					case *RTMPPacket:
						player.SendCachePacket(x)
					}
				}
			}

			player.isPlaying = true
			player.isIdling = false

			if player.gopPlayClear {
				s.rtmpGopCache = list.New()
				s.gopCacheSize = 0
				s.gopCacheDisabled = true
			}
		} else {
			LogRequest(idlePlayers[i].id, idlePlayers[i].ip, "Error: Invalid stream key provided")
			idlePlayers[i].SendStatusMessage(s.playStreamId, "error", "NetStream.Play.BadName", "Invalid stream key provided")
			idlePlayers[i].Kill()
		}

	}
}

// Starts a specific player
// Call only for publishers
// player - The player session
func (s *RTMPSession) StartPlayer(player *RTMPSession) {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if !s.isPublishing {
		player.isPlaying = false
		player.isIdling = true
		LogRequest(player.id, player.ip, "PLAY IDLE '"+player.channel+"'")
		return
	}

	LogRequest(player.id, player.ip, "PLAY START '"+player.channel+"'")

	player.SendMetadata(s.metaData, 0)
	player.SendAudioCodecHeader(s.audioCodec, s.aacSequenceHeader, 0)
	player.SendVideoCodecHeader(s.videoCodec, s.avcSequenceHeader, 0)

	if !player.gopPlayNo && s.rtmpGopCache.Len() > 0 {
		for t := s.rtmpGopCache.Front(); t != nil; t = t.Next() {
			chunks := t.Value
			switch x := chunks.(type) {
			case *RTMPPacket:
				player.SendCachePacket(x)
			}
		}
	}

	player.isPlaying = true
	player.isIdling = false

	if player.gopPlayClear {
		s.rtmpGopCache = list.New()
		s.gopCacheSize = 0
		s.gopCacheDisabled = true
	}
}

// Resumes a player that was paused
// Call only for publishers
// player - The player session
func (s *RTMPSession) ResumePlayer(player *RTMPSession) {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	player.SendAudioCodecHeader(s.audioCodec, s.aacSequenceHeader, s.clock)
	player.SendVideoCodecHeader(s.videoCodec, s.avcSequenceHeader, s.clock)
}

// Finishes a publishing session
// Call only for publishers
// isClose - True if it was closed due to a disconnection
func (s *RTMPSession) EndPublish(isClose bool) {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if s.isPublishing {

		LogRequest(s.id, s.ip, "PUBLISH END '"+s.channel+"'")

		if !isClose {
			s.SendStatusMessage(s.publishStreamId, "status", "NetStream.Unpublish.Success", s.GetStreamPath()+" is now unpublished.")
		}

		players := s.server.GetPlayers(s.channel)

		for i := 0; i < len(players); i++ {
			players[i].isIdling = true
			players[i].isPlaying = false
			LogRequest(players[i].id, players[i].ip, "PLAY IDLE '"+players[i].channel+"'")
			players[i].SendStatusMessage(players[i].playStreamId, "status", "NetStream.Play.UnpublishNotify", "stream is now unpublished.")
			players[i].SendStreamStatus(STREAM_EOF, players[i].playStreamId)
		}

		s.server.RemovePublisher(s.channel)

		s.rtmpGopCache = list.New()

		s.isPublishing = false

		// Send event
		if s.server.websocketControlConnection != nil {
			if s.server.websocketControlConnection.PublishEnd(s.channel, s.stream_id) {
				LogDebugSession(s.id, s.ip, "Stop event sent")
			} else {
				LogDebugSession(s.id, s.ip, "Could not send stop event")
			}
		} else {
			if s.SendStopCallback() {
				LogDebugSession(s.id, s.ip, "Stop event sent")
			} else {
				LogDebugSession(s.id, s.ip, "Could not send stop event")
			}
		}
	}
}

// Sets the clock for a publishing session
// clock - The value of the clock
func (s *RTMPSession) SetClock(clock int64) {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	s.clock = clock
}

// Sets the stream metadata that is being publishing
// metaData - The metadata
func (s *RTMPSession) SetMetaData(metaData []byte) {
	s.publish_mutex.Lock()
	defer s.publish_mutex.Unlock()

	if !s.isPublishing {
		return
	}

	s.metaData = metaData

	players := s.server.GetPlayers(s.channel)

	for i := 0; i < len(players); i++ {
		players[i].SendMetadata(metaData, 0)
	}
}
