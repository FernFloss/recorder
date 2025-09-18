package baseapp

import (
	general "RecoderAgent/internal/GeneralStructer"
	AudioRTSP "RecoderAgent/internal/RTSP/Audio"
	VideoRTSP "RecoderAgent/internal/RTSP/Video"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"
)

func memConsumed() uint64 {
	runtime.GC()
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	return s.Sys
}

type StreamConfig struct {
	Name        string `json:"name" binding:"required"`
	URL         string `json:"url" binding:"required"`
	Significant bool   `json:"significant" binding:"required"`
}

type Config struct {
	//название папки, куда будут класться исходники
	Room string `json:"room" binding:"required"`
	//название корневой папки, где будут папки записанных  Room-ов
	Destination string `json:"destination"`
	//Записи видео
	Videos []StreamConfig `json:"videos" binding:"required"`
	//Записи аудио
	Audios []StreamConfig `json:"audios" binding:"required"`
}

type Constant struct {
	GstreamerPath string `json:"Gstreamer_path"`
	RetryDelay    int    `json:"RetryDelay"`
	Port          int    `json:"AppPort"`
}

type controls struct {
	CTX         context.Context
	Cancel      context.CancelFunc
	startSignal chan interface{}
	stopSignal  chan interface{}
}

type mutexs struct {
	recordsMutex sync.Mutex //для контроля записи
	statusMutex  sync.Mutex //для изменения статуса - нужен как для мониторинга так и для битфокуса - моргать в случае проблемы
}

type App struct {
	currentConfig  Config
	status         string
	state          string //Для того чтобы битфокус определял запись идет -> надо остановить; записи нет -> можно запускать
	records        []general.Record
	WG             sync.WaitGroup
	mutexs         mutexs
	Constants      Constant
	ControlRuntime controls
}

var statusMap map[string]string
var stateMap map[string]string

func InitBaseApp() (*App, error) {
	statusMap = map[string]string{
		"-3":  "All stream failed",
		"-2":  "Crucial stream failed",
		"-1":  "Not recorders in memory",
		"0":   "Ready to record",
		"0.5": "Recording proccess is stoping",
		"1":   "Recordings",
	}
	stateMap = map[string]string{
		"0": "Nothing",
		"1": "Recording",
	}

	ctx, cancel := context.WithCancel(context.Background())

	a := &App{
		currentConfig: Config{},
		status:        "",               // пустая строка
		state:         "",               // пустая строка
		records:       nil,              // nil срез — допустимо, но лучше make([]..., 0)
		WG:            sync.WaitGroup{}, // можно оставить как есть — zero value безопасен
		mutexs: mutexs{
			statusMutex: sync.Mutex{}, // мьютекс не требует явной инициализации, но для порядка оставим
		},
		Constants: Constant{}, // нулевая структура
		ControlRuntime: controls{
			CTX:         ctx,
			Cancel:      cancel,
			startSignal: nil, // без буфера — если не планируешь множественные сигналы без чтения
			stopSignal:  nil,
		},
	}
	err := a.UpdateConstant()
	if err != nil {
		return nil, err
	}

	a.status = statusMap["-1"]
	a.state = stateMap["0"]
	return a, nil
}

func (appConfig *App) UpdateConstant() error {
	data, err := os.ReadFile("configs/constant.json")
	if err != nil {
		return err
	}
	var config Constant
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Не удалось загрузить конфигурацию: %v", err)
		return err
	}
	general.GstreamerPath = config.GstreamerPath
	general.RetryDelay = time.Duration(config.RetryDelay) * time.Second
	appConfig.Constants.Port = config.Port

	//По дефолту папки записей сохраняются в ./video
	if appConfig.currentConfig.Destination == "" {
		appConfig.currentConfig.Destination = "./videos"
	}

	if err = EnsureDestinationDirectoryExist(appConfig.currentConfig.Destination); err != nil {
		log.Fatalf("Не удалось создать папку для хранения записей: %v", err)
	}

	return nil
}

// Убеждаемся, что директория для хранения папок записей существует
func EnsureDestinationDirectoryExist(destination string) error {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		fmt.Println("Основная папка для хранения не найдена, создаем:", destination)
		err = os.MkdirAll(destination, 0755)
		if err != nil {
			return fmt.Errorf("ошибка создания папки %s: %v", destination, err)
		}
	}
	return nil
}

// Убеждаемся, что директория для хранения папки записи одной Room  существует
func EnsureRoomDirectoryExist(destination, room string) error {
	roomPath := filepath.Join(destination, room)

	if _, err := os.Stat(roomPath); os.IsNotExist(err) {
		fmt.Println("Папка для комнаты не найдена, создаем:", roomPath)
		err = os.MkdirAll(roomPath, 0755)
		if err != nil {
			return fmt.Errorf("ошибка создания папки для комнаты %s: %v", roomPath, err)
		}
	}
	return nil
}

func validateConfig(newConfig Config) error {
	//appConfig.recordsMutex.Lock()
	//defer appConfig.recordsMutex.Unlock()

	if len(newConfig.Videos) < 1 {
		return errors.New("at least one video stream must be specified")
	}

	// Регулярное выражение для проверки URL
	urlRTSPPattern := regexp.MustCompile(`^rtsp:\/\/(?:[a-zA-Z0-9_-]+:[^@]+@)?(\d{1,3}\.){3}\d{1,3}(:\d{1,5})?(\/\S*)?$`)
	//urlSRTPattern := regexp.MustCompile(`^srt:\/\/(?:[a-zA-Z0-9_-]+:[^@]+@)?(\d{1,3}\.){3}\d{1,3}(:\d{1,5})?(\/\S*)?$`)

	for i, v := range newConfig.Videos {
		if v.URL == "" {
			return fmt.Errorf("url not found in video stream %d", i+1)
		}
		if v.Name == "" {
			return fmt.Errorf("name not found name in video stream %d", i+1)
		}

		if !urlRTSPPattern.MatchString(v.URL) {
			return fmt.Errorf("invalid url format in video stream %d", i+1)
		}
	}

	if len(newConfig.Audios) != 1 {
		return errors.New("exactly one audio stream must be specified")
	}

	if newConfig.Audios[0].Name == "" {
		return errors.New("name not found name in audio stream")
	}
	if newConfig.Audios[0].URL == "" {
		return errors.New("url not found in audio stream")
	}

	if !urlRTSPPattern.MatchString(newConfig.Audios[0].URL) {
		return errors.New("invalid url format in audio stream")
	}

	// Название корневой папки, если не указано - ./videos
	if newConfig.Destination == "" {
		newConfig.Destination = "./videos"
	}

	//Проверяем существование папки у destination
	err := EnsureDestinationDirectoryExist(newConfig.Destination)
	if err != nil {
		return err
	}

	// Проверяем, что название комнаты указано.
	if newConfig.Room == "" {
		return errors.New("room`s name not found")
	}

	// проверяем существует или создаем директорию для Room
	err = EnsureRoomDirectoryExist(newConfig.Destination, newConfig.Room)
	if err != nil {
		return err
	}

	return nil
}

var ErrAlreadyRecording = fmt.Errorf("error in loadAndInitConfig: cannot initialize because state is recording, please stop records now")
var ErrConfigAlreadyLoaded = fmt.Errorf("error in loadAndInitConfig: cannot ")

func (appConfig *App) LoadAndInitConfig(nowConfig Config) error {
	appConfig.mutexs.recordsMutex.Lock()
	defer appConfig.mutexs.recordsMutex.Unlock()

	appConfig.mutexs.statusMutex.Lock()
	defer appConfig.mutexs.statusMutex.Unlock()
	if appConfig.state == "1" {
		return ErrAlreadyRecording
	}
	if len(appConfig.records) > 0 {
		return ErrConfigAlreadyLoaded
	}
	err := validateConfig(nowConfig)
	if err != nil {
		return err
	}
	appConfig.currentConfig = nowConfig
	appConfig.ControlRuntime.startSignal = make(chan interface{})
	appConfig.ControlRuntime.stopSignal = make(chan interface{})

	roomPath := filepath.Join(appConfig.currentConfig.Destination, appConfig.currentConfig.Room)
	fmt.Printf("BEFORE Init %.3fkb\n", float64(memConsumed())/1000)

	for _, v := range appConfig.currentConfig.Videos {
		appConfig.WG.Add(1)
		record, err := VideoRTSP.InitVideoRTSP(appConfig.ControlRuntime.CTX,
			&appConfig.WG, appConfig.ControlRuntime.startSignal, appConfig.ControlRuntime.stopSignal,
			roomPath, v.Name, v.URL, v.Significant)
		if err != nil {
			appConfig.StopRecords()
			//запуск очистки потоков !!!!!!!!!!!!!!
			return fmt.Errorf("error with %s in loadAndInitConfig: %v", v.Name, err)

		}
		appConfig.records = append(appConfig.records, record)
	}
	for _, a := range appConfig.currentConfig.Audios {
		appConfig.WG.Add(1)
		record, err := AudioRTSP.InitAudioRTSP(appConfig.ControlRuntime.CTX,
			&appConfig.WG, appConfig.ControlRuntime.startSignal, appConfig.ControlRuntime.stopSignal,
			roomPath, a.Name, a.URL, a.Significant)

		if err != nil {
			appConfig.StopRecords()
			//запуск очистки потоков для обнуления в nil для сборщика мусора!!!!!!!!!!!!!!
			return fmt.Errorf("error with %s in loadAndInitConfig: %v", a.Name, err)

		}
		appConfig.records = append(appConfig.records, record)

	}
	fmt.Printf("After Init %.3fkb\n", float64(memConsumed())/1000)

	//Где-то запускаем процесс мониторинга !!!!!
	appConfig.status = statusMap["0"]
	appConfig.state = stateMap["0"]
	log.Println(appConfig.currentConfig)

	return nil
}

var ErrRecordsIsEmpty = fmt.Errorf("the streams in memory not found")

func (appConfig *App) StartRecords() error {
	appConfig.mutexs.recordsMutex.Lock()
	defer appConfig.mutexs.recordsMutex.Unlock()

	appConfig.mutexs.statusMutex.Lock()
	defer appConfig.mutexs.statusMutex.Unlock()

	if len(appConfig.records) <= 0 {
		return ErrRecordsIsEmpty
	}
	if appConfig.state == "1" {

		return ErrAlreadyRecording
	}
	fmt.Printf("Before Start %.3fkb\n", float64(memConsumed())/1000)
	close(appConfig.ControlRuntime.startSignal)
	appConfig.status = statusMap["1"]
	appConfig.state = stateMap["1"]
	fmt.Printf("After Start %.3fkb\n", float64(memConsumed())/1000)
	return nil
}

func (appConfig *App) StopRecords() error {
	appConfig.mutexs.recordsMutex.Lock()
	defer appConfig.mutexs.recordsMutex.Unlock()

	appConfig.mutexs.statusMutex.Lock()
	defer appConfig.mutexs.statusMutex.Unlock()

	if len(appConfig.records) <= 0 {
		return ErrRecordsIsEmpty
	}
	if appConfig.state == "1" {
		appConfig.status = statusMap["0.5"]
	}
	log.Println("The record is stoping...")
	fmt.Printf("BEFORE Stop %.3fkb\n", float64(memConsumed())/1000)
	close(appConfig.ControlRuntime.stopSignal)
	appConfig.WG.Wait()
	fmt.Printf("AFTER Stop %.3fkb\n", float64(memConsumed())/1000)
	appConfig.clearMeta()
	fmt.Printf("AFTER Stop and CLEAN %.3fkb\n", float64(memConsumed())/1000)
	return nil
}

func (appConfig *App) clearMeta() {
	appConfig.records = nil
	appConfig.ControlRuntime.startSignal = nil
	appConfig.ControlRuntime.stopSignal = nil

}
