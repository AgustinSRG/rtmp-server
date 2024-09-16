// RTMP callback

package main

import (
	"fmt"
	"os"
	"time"

	"net/http"

	"github.com/golang-jwt/jwt"
)

const JWT_EXPIRATION_TIME_SECONDS = 120

func (s *RTMPSession) SendStartCallback() bool {
	JWT_SECRET := os.Getenv("JWT_SECRET")
	CALLBACK_URL := os.Getenv("CALLBACK_URL")

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
		"sub":       subject,
		"event":     "start",
		"channel":   s.channel,
		"key":       s.key,
		"client_ip": s.ip,
		"rtmp_host": s.server.host,
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
		LogDebugSession(s.id, s.ip, "Callback request ended with status code: "+fmt.Sprint(res.StatusCode))
		return false
	}

	s.stream_id = res.Header.Get("stream-id")
	LogDebugSession(s.id, s.ip, "Stream ID: "+s.stream_id)

	return true
}

func (s *RTMPSession) SendStopCallback() bool {
	JWT_SECRET := os.Getenv("JWT_SECRET")
	CALLBACK_URL := os.Getenv("CALLBACK_URL")

	if CALLBACK_URL == "" {
		return true // No callback
	}

	LogDebugSession(s.id, s.ip, "POST "+CALLBACK_URL+" | Event: STOP | Channel: "+s.channel)

	var subject = os.Getenv("CUSTOM_JWT_SUBJECT")

	if subject == "" {
		subject = "rtmp_event"
	}

	exp := time.Now().Unix() + JWT_EXPIRATION_TIME_SECONDS
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":       subject,
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
		LogDebugSession(s.id, s.ip, "Callback request ended with status code: "+fmt.Sprint(res.StatusCode))
		return false
	}

	return true
}
