package main

import "github.com/joho/godotenv"

func main() {
	_ = godotenv.Load() // Load env vars

	LogInfo("RTMP Go Server (Version 1.0.0)")

	server := CreateRTMPServer()

	go setupRedisCommandReceiver(server)

	if server != nil {
		server.Start()
	}
}
