package main

func main() {
	LogInfo("RTMP Go Server (Version 1.0.0)")

	server := CreateRTMPServer()

	if server != nil {
		server.Start()
	}
}
