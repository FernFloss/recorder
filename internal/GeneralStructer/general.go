package generalstructer

import (
	"context"
	"os"
	"time"
)

// Глобальные переменные
var GstreamerPath = "gst-launch-1.0"
var RetryDelay = 5 * time.Second

type Status struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}
type MonitoringState struct {
	State       string
	Significant bool
	Errors      string
}

type Record interface {
	SendMonitoringInfo() <-chan MonitoringState
}

func CreateFile(path, name, recordtype string) (*os.File, error) {
	filename := name + "__" + recordtype + "__status.json"

	file, err := os.Create(path + "/" + filename)
	if err != nil {
		return file, err
	}
	return file, nil
}

func Or(ctx context.Context, channelDone <-chan interface{}) <-chan interface{} {
	orDone := make(chan interface{})
	go func() {
		defer close(orDone)
		select {
		case <-ctx.Done():
		case <-channelDone:
		}
	}()
	return orDone
}
