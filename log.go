// Logs

package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var LOG_MUTEX = sync.Mutex{}

var LOG_DEBUG_ENABLED = false
var LOG_REQUESTS_ENABLED = false

func InitLog() {
	LOG_DEBUG_ENABLED = (os.Getenv("LOG_DEBUG") == "YES")
	LOG_REQUESTS_ENABLED = (os.Getenv("LOG_REQUESTS") != "NO")
}

func LogLine(line string) {
	tm := time.Now()
	LOG_MUTEX.Lock()
	defer LOG_MUTEX.Unlock()
	fmt.Printf("[%s] %s\n", tm.Format("2006-01-02 15:04:05"), line)
}

func LogWarning(line string) {
	LogLine("[WARNING] " + line)
}

func LogInfo(line string) {
	LogLine("[INFO] " + line)
}

func LogError(err error) {
	LogLine("[ERROR] " + err.Error())
}

func LogErrorMessage(line string) {
	LogLine("[ERROR] " + line)
}

func LogRequest(session_id uint64, ip string, line string) {
	if LOG_REQUESTS_ENABLED {
		LogLine("[REQUEST] #" + strconv.Itoa(int(session_id)) + " (" + ip + ") " + line)
	}
}

func LogDebug(line string) {
	if LOG_DEBUG_ENABLED {
		LogLine("[DEBUG] " + line)
	}
}

func LogDebugSession(session_id uint64, ip string, line string) {
	if LOG_DEBUG_ENABLED {
		LogLine("[DEBUG] #" + strconv.Itoa(int(session_id)) + " (" + ip + ") " + line)
	}
}
