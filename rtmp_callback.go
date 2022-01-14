// RTMP callback

package main

import (
	"os"
	"time"

	"net/http"

	"github.com/golang-jwt/jwt"
)

const JWT_EXPIRATION_TIME_SECONDS = 120

var JWT_SECRET = os.Getenv("JWT_SECRET")
var CALLBACK_URL = os.Getenv("CALLBACK_URL")

func (s *RTMPSession) SendStartCallback() bool {
	if CALLBACK_URL == "" {
		return true // No callback
	}

	LogDebugSession(s.id, s.ip, "POST "+CALLBACK_URL+" | Event: START | Channel: "+s.channel)

	var subject = os.Getenv("CUSTOM_JWT_SUBJECT")

	if subject == "" {
		subject = "rtmp_event"
	}

	exp := time.Now().Unix() + JWT_EXPIRATION_TIME_SECONDS
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":       "rtmp_event",
		"event":     "start",
		"channel":   s.channel,
		"key":       s.key,
		"client_ip": s.ip,
		"rtmp_port": s.server.port,
		"exp":       exp,
	})

	tokenb64, e := token.SignedString([]byte(JWT_SECRET))

	if e != nil {
		LogError(e)
		return false
	}

	client := &http.Client{}

	req, e := http.NewRequest("POST", CALLBACK_URL, nil)

	if e != nil {
		LogError(e)
		return false
	}

	req.Header.Set("rtmp-event", tokenb64)

	res, e := client.Do(req)

	if e != nil {
		LogError(e)
		return false
	}

	if res.StatusCode != 200 {
		return false
	}

	s.stream_id = res.Header.Get("stream-id")
	LogDebugSession(s.id, s.ip, "Stream ID: "+s.stream_id)

	return true
}

func (s *RTMPSession) SendStopCallback() bool {
	if CALLBACK_URL == "" {
		return true // No callback
	}

	LogDebugSession(s.id, s.ip, "POST "+CALLBACK_URL+" | Event: STOP | Channel: "+s.channel)

	exp := time.Now().Unix() + JWT_EXPIRATION_TIME_SECONDS
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":       "rtmp_event",
		"event":     "stop",
		"channel":   s.channel,
		"key":       s.key,
		"stream_id": s.stream_id,
		"client_ip": s.ip,
		"exp":       exp,
	})

	tokenb64, e := token.SignedString([]byte(JWT_SECRET))

	if e != nil {
		LogError(e)
		return false
	}

	client := &http.Client{}

	req, e := http.NewRequest("POST", CALLBACK_URL, nil)

	if e != nil {
		LogError(e)
		return false
	}

	req.Header.Set("rtmp-event", tokenb64)

	res, e := client.Do(req)

	if e != nil {
		LogError(e)
		return false
	}

	if res.StatusCode != 200 {
		return false
	}

	return true
}
