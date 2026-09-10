package backend

import (
	"os"
	"strings"
)

func hikariDebugEnabled() bool {
	level := strings.ToLower(os.Getenv("HIKARI_LOG_LEVEL"))
	return level == "debug" || level == "trace"
}

func hikariTraceEnabled() bool {
	return strings.EqualFold(os.Getenv("HIKARI_LOG_LEVEL"), "trace")
}
