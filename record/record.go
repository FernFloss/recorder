package record

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Глобальные переменные
var GstreamerPath = "C:/archive/gstreamer/1.0/mingw_x86_64/bin/gst-launch-1.0.exe"
var RetryDelay = 5 // секунды
var PathStore = "./videos"

// Status – запись состояния с меткой времени.
type Status struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// Типы команд для Record.
type CommandType string

const (
	CmdStart CommandType = "start"
	CmdStop  CommandType = "stop"
)

// RecordCommand – команда для Record.
// Для CmdGetState ResponseChan используется для возврата результата.
type RecordCommand struct {
	Action       CommandType
	ResponseChan chan string
}

// Record – класс для управления записью.
type Record struct {
	Type     string `json:"type: video or audio"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	URL_type string
	History  []Status `json:"history"`

	// State – массив из двух строк:
	// [0] – текущее состояние ("on", "off", "error"),
	// [1] – сообщение об ошибке (если есть, иначе "nil")
	State []string

	isRunning  bool
	shouldStop bool

	// Используем stateMutex только для защиты доступа к State.
	stateMutex sync.Mutex

	cmd         *exec.Cmd
	cancelFunc  context.CancelFunc
	CommandChan chan RecordCommand
}

// NewRecord – конструктор для Record.
func NewRecord(recordType, recordName, recordUrl, recordUrlType string) *Record {
	return &Record{
		Type:        recordType,
		Name:        recordName,
		URL:         recordUrl,
		URL_type:    recordUrlType,
		History:     []Status{},
		State:       []string{"off", "nil"},
		CommandChan: make(chan RecordCommand),
	}
}

// updateStatusFile сохраняет последнюю запись истории в JSON-файл с именем "<Name>__status.json".
func (r *Record) updateStatusFile() {
	// Если история пуста, выходим.
	if len(r.History) == 0 {
		return
	}

	data, err := json.MarshalIndent(r.History, "", "  ")
	if err != nil {
		fmt.Println("Ошибка маршалинга истории:", err)
		return
	}
	filename := r.Name + "__" + r.Type + "__status.json"
	if err := os.WriteFile(PathStore+"/"+filename, data, 0644); err != nil {
		fmt.Println("Ошибка записи файла статуса:", err)
	}
}

// startGStreamerWithFilename запускает GStreamer с формированием имени файла для записи.
func (r *Record) startGStreamerWithFilename(ctx context.Context, filename string) {

	if r.URL_type == "rtsp" && r.Type == "video" {
		r.cmd = exec.CommandContext(ctx, GstreamerPath,
			"rtspsrc", "location="+r.URL, "protocols=tcp", "latency=200", "drop-on-latency=true",
			"!", "queue", "!", "rtph264depay", "!", "h264parse", "!", "tee", "name=t",
			"t.", "!", "queue", "!", "matroskamux", "!", "filesink",
			"location="+PathStore+"/"+r.Name+"/"+filename)
	} else if r.URL_type == "srt" {
		r.cmd = exec.CommandContext(ctx, GstreamerPath,
			"srtsrc", "uri="+r.URL, "latency=200",
			"!", "queue", "!", "rtph264depay", "!", "h264parse", "!", "tee", "name=t",
			"t.", "!", "queue", "!", "matroskamux", "!", "filesink", "location="+PathStore+"/"+r.Name+"/"+filename)
	} else if r.URL_type == "rtsp" && r.Type == "audio" {
		r.cmd = exec.CommandContext(ctx, GstreamerPath, "rtspsrc", "location="+r.URL, "protocols=tcp",
			"latency=200", "!", "queue", "!", "rtpmp4gdepay", "!", "aacparse", "!", "matroskamux", "!", "filesink", "location="+PathStore+"/"+r.Name+"/"+filename)
	}

}

func (r *Record) updateState(newState, errMsg string) {
	r.stateMutex.Lock()
	defer r.stateMutex.Unlock()
	r.State[0] = newState
	r.State[1] = errMsg
}

func (r *Record) GetState() string {
	r.stateMutex.Lock()
	defer r.stateMutex.Unlock()
	return r.State[0]
}

func (r *Record) GetError() string {
	r.stateMutex.Lock()
	defer r.stateMutex.Unlock()
	return r.State[1]
}

func (r *Record) startRecord() {
	for {
		if r.shouldStop {
			fmt.Println("Запись остановлена вручную, выходим из цикла.")
			return
		}
		startTime := time.Now().Add(1 * time.Second)
		filename := fmt.Sprintf("%s_%s_%s.mkv", r.Name, r.Type, startTime.Format("20060102_150405"))
		fmt.Printf("Запуск GStreamer для %s по URL: %s, запись в файл: %s\n", r.Name, r.URL, filename)

		ctx, cancel := context.WithCancel(context.Background())
		r.cancelFunc = cancel

		r.startGStreamerWithFilename(ctx, filename)

		// Перенаправляем `stderr` в пайп
		stderr, err := r.cmd.StderrPipe()
		if err != nil {
			fmt.Println("Ошибка перенаправления stderr:", err)
			return
		}

		// Запускаем команду
		if err := r.cmd.Start(); err != nil {
			fmt.Println("Ошибка запуска GStreamer:", err)
			r.isRunning = false
			r.updateState("error", err.Error())
		}

		// Читаем `stderr` в реальном времени и проверяем ошибки
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Println("Ошибка GStreamer:", r.Name, line)

				// Если в stderr есть критическая ошибка – обновляем статус
				if strings.Contains(line, "ERROR") || strings.Contains(line, "Could not read from resource") {
					r.updateState("error", line)
					r.isRunning = false
					return
				}
			}
		}()

		r.isRunning = true
		r.History = append(r.History, Status{"started", startTime})
		r.updateState("on", "nil")
		r.updateStatusFile()

		// Ждем завершения процесса
		err = r.cmd.Wait()

		if ctx.Err() == context.Canceled {
			fmt.Println("GStreamer был принудительно остановлен.")
			r.updateState("off", "nil")
			r.isRunning = false
			r.History = append(r.History, Status{"stopped", time.Now()})
			r.updateStatusFile()
			return
		}
		if err != nil {
			fmt.Println("GStreamer завершился с ошибкой:", err)
			// Проверяем размер файла
			fileInfo, _ := os.Stat(PathStore + "/" + r.Name + "/" + filename)
			fileSize := fileInfo.Size()

			if fileSize < 1024 {
				fmt.Println("Файл слишком маленький, удаляем:", PathStore+"/"+r.Name+"/"+filename)
				os.Remove(PathStore + "/" + r.Name + "/" + filename)
				if len(r.History) > 0 {
					r.History = r.History[:len(r.History)-1]
					r.updateStatusFile()
				}
			} else {
				r.History = append(r.History, Status{"stopped", time.Now().Add(-25 * time.Second)})
				r.updateStatusFile()

			}
		}
		r.isRunning = false
		fmt.Printf("GStreamer остановился, перезапуск через %d секунд\n", RetryDelay)
		// Ожидание перед перезапуском, если не было команды Stop
		for i := 0; i < RetryDelay; i++ {
			if r.shouldStop {
				fmt.Println("Получена команда остановки, выходим из цикла перезапуска.")
				return
			}
			time.Sleep(1 * time.Second)
		}

		fmt.Println("Перезапуск GStreamer...")
	}

}

// Run – главный цикл Record, который слушает канал команд и выполняет их..
func (r *Record) Run() {
	for {
		select {
		case cmd := <-r.CommandChan:
			switch cmd.Action {
			case CmdStart:
				if !r.isRunning { // Проверяем, не запущена ли уже запись
					go r.startRecord()
				}
			case CmdStop:
				// При команде Stop устанавливаем флаг и останавливаем процесс, если он работает.
				r.shouldStop = true
				if r.cancelFunc != nil {
					r.cancelFunc()
				}
				fmt.Println("ОСТАНОВЛЕНО", r.Name)

				r.updateState("off", "nil")
				time.Sleep(10 * time.Second)
				r.ClearHistory()
				return
			}
		default:
			// Если никаких команд нет, просто ждем немного, чтобы цикл не загружал процессор.
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (r *Record) ClearHistory() {
	r.History = []Status{}
	r.updateState("off", "nil")
	fmt.Println("История очищена")
}
