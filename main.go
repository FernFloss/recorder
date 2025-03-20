package main

import (
	"agent/record"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type StreamConfig struct {
	Type     string `json:"type" binding:"required"`
	Name     string `json:"name" binding:"required"`
	URL      string `json:"url" binding:"required"`
	URL_type string `json:"url_type"`
}

type Config struct {
	Room        string         `json:"room" binding:"required"`
	Videos      []StreamConfig `json:"videos" binding:"required"`
	Audios      []StreamConfig `json:"audios" binding:"required"`
	Destination string         `json:"destination"`
}

type Constant struct {
	GstreamerPath string `json:"Gstreamer_path"`
	PathStore     string `json:"PathStore"`
	RetryDelay    string `json:"RetryDelay"`
}

var (
	currentConfig Config
	state         string
	records       []*record.Record
	recordsMutex  sync.Mutex
	status        string
)

var recordStateGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "record_state",
		Help: "Состояние записи: 1 = on, 0 = off, -1 = error",
	},
	[]string{"name", "url", "state", "error"},
)

var roomStateGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "room_state",
		Help: "Состояние записи: 1 = Идет запись, 0,5 = работает хотя бы 1 видео и аудио поток 0 = Стоп , -1 = все упало",
	},
	[]string{"Room", "state"},
)

func init() {
	prometheus.MustRegister(recordStateGauge)
	prometheus.MustRegister(roomStateGauge)
	err := loadConfigFromFile()
	if err != nil {
		fmt.Println("Ошибка загрузки конфигурации:", err)
		status = "Not found config in memory"
		return
	}
	err = updateConstant()
	if err != nil {
		fmt.Println("Ошибка загрузки констант:", err)
		status = "Not found constant in json file"
		return
	}
	status = "Ready o record"

}

func updateConstant() error {
	data, err := os.ReadFile("constant.json")
	if err != nil {
		return err
	}
	var config Constant
	if err := json.Unmarshal(data, &config); err != nil {
		status = "Not found constant in json file"
		return err
	}

	record.GstreamerPath = config.GstreamerPath
	record.PathStore = config.PathStore
	retryDelayInt, err := strconv.Atoi(config.RetryDelay)
	if err != nil {
		return fmt.Errorf("invalid RetryDelay value: %v", err)
	}
	record.RetryDelay = retryDelayInt
	return nil
}

func validateConfig(newConfig Config) error {
	recordsMutex.Lock()
	defer recordsMutex.Unlock()

	// Проверяем, что название комнаты указано.
	if newConfig.Room == "" {
		return errors.New("Room`s name not found")
	}

	if newConfig.Destination == "" {
		newConfig.Destination = "./videos"
	}

	// Регулярное выражение для проверки URL (должен начинаться с rtsp:// или srt://).
	urlRTSPPattern := regexp.MustCompile(`^rtsp:\/\/(?:[a-zA-Z0-9_-]+:[^@]+@)?(\d{1,3}\.){3}\d{1,3}(:\d{1,5})?(\/\S*)?$`)
	urlSRTPattern := regexp.MustCompile(`^srt:\/\/(?:[a-zA-Z0-9_-]+:[^@]+@)?(\d{1,3}\.){3}\d{1,3}(:\d{1,5})?(\/\S*)?$`)

	validVideos := []StreamConfig{}
	for i, v := range newConfig.Videos {
		if v.URL == "" {
			return fmt.Errorf("URL not found in video stream %d", i+1)
		}
		if v.Name == "" {
			return fmt.Errorf("Name not found name in video stream %d", i+1)
		}

		if urlRTSPPattern.MatchString(v.URL) {
			v.URL_type = "rtsp"
		} else if urlSRTPattern.MatchString(v.URL) {
			v.URL_type = "srt"
		} else {
			return fmt.Errorf("Invalid URL format in video stream %d", i+1)
		}
		validVideos = append(validVideos, v)
	}

	if len(validVideos) < 1 {
		return errors.New("At least one video stream must be specified")
	}

	if len(newConfig.Audios) != 1 {
		return errors.New("Exactly one audio stream must be specified")
	}

	audio := newConfig.Audios[0]
	if audio.Name == "" {
		return errors.New("Name not found name in audio stream")
	}
	if audio.URL == "" {
		return errors.New("URL not found in audio stream")
	}

	if urlRTSPPattern.MatchString(audio.URL) {
		audio.URL_type = "rtsp"
	} else if urlSRTPattern.MatchString(audio.URL) {
		audio.URL_type = "srt"
	} else {
		return errors.New("Invalid URL format in audio stream")
	}

	currentConfig = newConfig

	// Очищаем массив записей
	records = []*record.Record{}
	for _, v := range currentConfig.Videos {
		rec := record.NewRecord(v.Type, v.Name, v.URL, v.URL_type)
		records = append(records, rec)
		go rec.Run()
	}
	a := currentConfig.Audios[0]
	rec := record.NewRecord(a.Type, a.Name, a.URL, a.URL_type)
	records = append(records, rec)
	go rec.Run()

	err := EnsureAllDirectoriesExist()
	if err != nil {
		return err
	}
	status = "Ready to record"

	fmt.Printf("Конфигурация загружена в память: %+v\n", currentConfig)
	return nil
}

func EnsureAllDirectoriesExist() error {
	if _, err := os.Stat(record.PathStore); os.IsNotExist(err) {
		fmt.Println("Основная папка не найдена, создаем:", record.PathStore)
		err = os.MkdirAll(record.PathStore, 0755)
		if err != nil {
			return fmt.Errorf("Ошибка создания папки %s: %v\n", record.PathStore, err)
		}
	}

	for _, rec := range records {
		// Полный путь к подпапке записи
		recordDir := filepath.Join(record.PathStore, rec.Name)

		// Проверяем и создаем подпапку для записи
		if _, err := os.Stat(recordDir); os.IsNotExist(err) {
			fmt.Println("Папка записи не найдена, создаем:", recordDir)
			err = os.MkdirAll(recordDir, 0755)
			if err != nil {
				return fmt.Errorf("Ошибка создания папки %s: %v\n", recordDir, err)
			}
		} else {
			// Проверяем, есть ли файлы в папке
			files, err := os.ReadDir(recordDir)
			if err != nil {
				return fmt.Errorf("Ошибка проверки содержимого папки %s: %v\n", recordDir, err)
			}

			// надо придумать как сделать предупреждение
			if len(files) > 0 {
				fmt.Printf("⚠️ ПРЕДУПРЕЖДЕНИЕ: Папка %s уже содержит файлы!\n", recordDir)
			}
		}
	}
	return nil
}

func loadConfigFromFile() error {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	err = validateConfig(config)
	if err != nil {
		return err

	}
	err = updateConstant()
	if err != nil {
		return err
	}
	return nil
}

func configHandler(c *gin.Context) {
	var config Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Проверяем, есть ли уже работающие потоки
	if len(records) > 0 {
		running := false
		for _, rec := range records {
			if rec.GetState() == "on" {
				running = true
				break
			}
		}
		// Если уже есть работающие потоки, не запускаем повторно
		if running {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Already running,stop records first before loading a new configuration."})
			return
		}
	}

	err := validateConfig(config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = updateConstant()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = saveConfigToFile(config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Конфигурация получена, сохранена и загружена в память",
		"config":  currentConfig,
	})
}

func configFromFileHandler(c *gin.Context) {
	err := loadConfigFromFile()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Конфигурация получена, сохранена и загружена в память",
		"config":  currentConfig,
	})

}

func saveConfigToFile(config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", data, 0644)
}

func startHandler(c *gin.Context) {
	recordsMutex.Lock()
	defer recordsMutex.Unlock()
	running := false
	for _, rec := range records {
		if rec.GetState() == "on" {
			running = true
			break
		}
	}
	if running {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Запись уже идет, повторный запуск невозможен"})
		return
	}

	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет активных записей для запуска"})
		return
	}

	// Канал для одновременного старта
	startSignal := make(chan struct{})

	// Запускаем горутины, которые будут ждать сигнала
	for _, rec := range records {
		go func(r *record.Record) {
			<-startSignal
			r.CommandChan <- record.RecordCommand{Action: record.CmdStart}
		}(rec)
	}

	// Закрываем канал, чтобы все потоки стартовали одновременно
	close(startSignal)

	// Запускаем мониторинг, если он еще не активен
	monitorOnce.Do(func() {
		go monitorRecords()
	})

	c.JSON(http.StatusOK, gin.H{"message": "Все записи запущены"})
}

var (
	stopMonitoring chan struct{}
	monitorOnce    sync.Once
	monitorMutex   sync.Mutex
)

func monitorRecords() {
	monitorMutex.Lock()
	if stopMonitoring != nil {
		monitorMutex.Unlock()
		fmt.Println("Мониторинг уже запущен")
		return
	}
	stopMonitoring = make(chan struct{})
	monitorMutex.Unlock()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	fmt.Println("Мониторинг запущен")

	for {
		select {
		case <-stopMonitoring:
			fmt.Println("Мониторинг остановлен")
			recordStateGauge.Reset()
			roomStateGauge.Reset()
			return
		case <-ticker.C:
			var wg sync.WaitGroup

			recordsMutex.Lock()
			select {
			case <-stopMonitoring:
				recordStateGauge.Reset()
				roomStateGauge.Reset()
				fmt.Println("Мониторинг остановлен во время обновления метрик")
				recordsMutex.Unlock()
				return
			default:
			}
			recordStateGauge.Reset()
			roomStateGauge.Reset()

			var audioWorking, allVideosFailed, allVideoWorking int32 = 0, 1, 1

			for _, rec := range records {
				wg.Add(1)
				go func(r *record.Record) {
					defer wg.Done()
					state := r.GetState()
					errorMsg := r.GetError()
					value := 0.0

					if state == "on" {
						value = 1.0
						if atomic.LoadInt32(&audioWorking) == 0 && r.Type == "audio" {
							atomic.StoreInt32(&audioWorking, 1)
						}
						if atomic.LoadInt32(&allVideosFailed) == 1 && r.Type == "video" {
							atomic.StoreInt32(&allVideosFailed, 0)
						}
					} else if state == "error" {
						value = -1.0
						if atomic.LoadInt32(&allVideoWorking) == 1 && r.Type == "video" {
							atomic.StoreInt32(&allVideoWorking, 0)
						}
					}

					recordStateGauge.WithLabelValues(r.Name, r.URL, state, errorMsg).Set(value)
				}(rec)
			}

			recordsMutex.Unlock()
			wg.Wait()

			// Используем atomic.LoadInt32 для безопасного чтения
			if atomic.LoadInt32(&audioWorking) == 1 && atomic.LoadInt32(&allVideoWorking) == 1 {
				roomStateGauge.WithLabelValues(currentConfig.Room, "All working").Set(1)
			} else if atomic.LoadInt32(&audioWorking) == 1 && atomic.LoadInt32(&allVideosFailed) == 0 {
				roomStateGauge.WithLabelValues(currentConfig.Room, "Audio and at least 1 video working").Set(0.5)
			} else {
				roomStateGauge.WithLabelValues(currentConfig.Room, "Audio or all videos not working").Set(-1)
			}

		}
	}
}

func stopHandler(c *gin.Context) {
	recordsMutex.Lock()
	defer recordsMutex.Unlock()

	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет активных записей для остановки"})
		return
	}

	monitorMutex.Lock()
	if stopMonitoring != nil {
		close(stopMonitoring)
		stopMonitoring = nil
		fmt.Println("Мониторинг корректно остановлен")
	}
	monitorMutex.Unlock()

	stopSignal := make(chan struct{})
	for _, rec := range records {
		go func(r *record.Record) {
			<-stopSignal // Ждем закрытия канала
			r.CommandChan <- record.RecordCommand{Action: record.CmdStop}
		}(rec)
	}
	close(stopSignal)

	// Очищаем массив записей
	records = []*record.Record{}

	c.JSON(http.StatusOK, gin.H{"message": "Все записи остановлены, история очищена"})
	status = "Not found config in memory"
}

func statusHandler(c *gin.Context) {
	recordsMutex.Lock()
	defer recordsMutex.Unlock()

	var statuses []map[string]string

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to gather Prometheus metrics"})
		return
	}

	for _, metricFamily := range metricFamilies {

		if metricFamily.GetName() == "record_state" {
			for _, metric := range metricFamily.GetMetric() {
				name, url, errorMsg := "", "", ""

				// Получаем все метки (лейблы)
				for _, label := range metric.GetLabel() {
					switch label.GetName() {
					case "name":
						name = label.GetValue()
					case "url":
						url = label.GetValue()

					case "error":
						errorMsg = label.GetValue()
					}
				}

				// Определяем состояние потока
				value := metric.GetGauge().GetValue()
				finalState := "unknown"
				if value == 1 {
					finalState = "on"
				} else if value == -1 {
					finalState = "error"
				} else {
					finalState = "off"
				}

				// Добавляем в список статусов
				statuses = append(statuses, map[string]string{
					"name":  name,
					"url":   url,
					"state": finalState,
					"error": errorMsg,
				})
			}
		}
	}

	// Отправляем JSON-ответ с массивом статусов
	c.JSON(200, gin.H{"records": statuses})
}

func main() {
	router := gin.Default()

	// API эндпоинты
	router.POST("/config", configHandler)
	router.GET("/start", startHandler)
	router.GET("/stop", stopHandler)
	router.GET("/status", statusHandler)
	router.GET("/configFromFile", configFromFileHandler)

	// Эндпоинт для Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Раздача статических файлов из папки static.
	router.Static("/static", "./static")
	// Редирект с "/" на "/static/index.html"
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/static/index.html")
	})

	fmt.Println("Сервер запущен на порту :5000")
	router.Run(":5000")
}
