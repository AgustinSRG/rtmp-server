package main

import "github.com/joho/godotenv"

func main() {
	_ = godotenv.Load() // Load env vars

	InitLog() // Initializes log utils

	LogInfo("RTMP Server (Golang Implementation)")

	server := CreateRTMPServer()

	go setupRedisCommandReceiver(server)

	if server != nil {
		server.Start()
	}
}
