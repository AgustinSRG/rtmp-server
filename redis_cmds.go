// Redis commands

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func setupRedisCommandReceiver(server *RTMPServer) {
	useRedis := os.Getenv("REDIS_USE")

	if useRedis != "YES" {
		return // Not using redis
	}

	defer func() {
		if err := recover(); err != nil {
			switch x := err.(type) {
			case string:
				LogError(errors.New(x))
			case error:
				LogError(x)
			default:
				LogError(errors.New("could not connect to redis"))
			}
		}
		LogWarning("Connection to Redis lost!")
	}()

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisChannel := os.Getenv("REDIS_CHANNEL")

	if redisChannel == "" {
		redisChannel = "rtmp_commands"
	}

	redisTLS := os.Getenv("REDIS_TLS")

	ctx := context.Background()

	var redisClient *redis.Client

	if redisTLS == "YES" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:      redisHost + ":" + redisPort,
			Password:  redisPassword,
			TLSConfig: &tls.Config{},
		})
	} else {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisHost + ":" + redisPort,
			Password: redisPassword,
		})
	}

	subscriber := redisClient.Subscribe(ctx, redisChannel)

	LogInfo("[REDIS] Listening for commands on channel '" + redisChannel + "'")

	for {
		msg, err := subscriber.ReceiveMessage(ctx)

		if err != nil {
			LogWarning("Could not connect to Redis: " + err.Error())
			time.Sleep(10 * time.Second)
		} else {
			// Parse message
			parseRedisCommand(server, msg.Payload)
		}
	}
}

func parseRedisCommand(server *RTMPServer, cmd string) {
	defer func() {
		if err := recover(); err != nil {
			switch x := err.(type) {
			case string:
				LogError(errors.New(x))
			case error:
				LogError(x)
			default:
				LogError(errors.New("parsing error"))
			}
		}
		LogWarning("Could not parse message: " + cmd)
	}()

	parts := strings.Split(cmd, ">")
	if len(parts) != 2 {
		LogWarning("Invalid message from Redis: " + cmd)
		return // Invalid message
	}

	cmdName := parts[0]
	cmdArgs := strings.Split(parts[1], "|")

	switch cmdName {
	case "kill-session":
		if len(cmdArgs) < 1 {
			LogWarning("Invalid message from Redis: " + cmd)
			return
		}
		channel := cmdArgs[0]
		publisher := server.GetPublisher(channel)

		if publisher != nil {
			publisher.Kill()
		}
	case "close-stream":
		if len(cmdArgs) < 2 {
			LogWarning("Invalid message from Redis: " + cmd)
			return
		}

		channel := cmdArgs[0]
		streamId := cmdArgs[1]
		publisher := server.GetPublisher(channel)

		if publisher != nil && publisher.stream_id == streamId {
			publisher.Kill()
		}
	default:
		LogWarning("Unknown Redis command: " + cmd)
	}
}
