package main

import (
	"RecoderAgent/api"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	BaseApp "RecoderAgent/internal/BaseApp"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// func main() {
// 	memConsumed := func() uint64 {
// 		runtime.GC()
// 		var s runtime.MemStats
// 		runtime.ReadMemStats(&s)
// 		return s.Sys
// 	}
// 	before := memConsumed()
// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()
// 	var wg sync.WaitGroup
// 	var err error
// 	var PathStore = "./videos/555"
// 	startSignal := make(chan interface{})
// 	stopSignal := make(chan interface{})
// 	records := make([]general.Record, 5)
// 	wg.Add(2)
// 	records[0], err = stream.InitVideoRTSP(ctx, &wg, startSignal, stopSignal, PathStore, "102", "rtsp://172.18.191.102:554/user=admin_password=BhcGS01Q_channel=1_stream=0&protocol=unicast.sdp?real_stream")
// 	if err != nil {
// 		fmt.Printf("ERROR with init 1 : %v\n", err)
// 	}
// 	records[1], err = stream.InitVideoRTSP(ctx, &wg, startSignal, stopSignal, PathStore, "53", "rtsp://17.18.191.53:554/Streaming/Chanels/1")
// 	if err != nil {
// 		fmt.Printf("ERROR with init 2 : %v\n", err)
// 	}
// 	middle := memConsumed()
// 	time.Sleep(5 * time.Second)
// 	fmt.Printf("закрываем канал на 10 с\n")
// 	close(startSignal)
// 	middlePrEWORK := memConsumed()
// 	time.Sleep(45 * time.Second)
// 	middleWORK := memConsumed()
// 	close(stopSignal)
// 	wg.Wait()
// 	end := memConsumed()
// 	fmt.Printf("Наступил финал \n")
// 	fmt.Printf("BEFORE %.3fkb\n", float64(before)/1000)
// 	fmt.Printf("middle %.3fkb\n", float64(middle)/1000)
// 	fmt.Printf("middlePrEWORK %.3fkb\n", float64(middlePrEWORK)/1000)
// 	fmt.Printf("middleWORK %.3fkb\n", float64(middleWORK)/1000)
// 	fmt.Printf("end %.3fkb\n", float64(end)/1000)

// }

var recordStateGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "record_state",
		Help: "Состояние записи: 1 = on, 0 = off, -1 = error, -2 = critical error",
	},
	[]string{"name", "url", "state", "error"},
)

var roomStateGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "room_state",
		Help: "Состояние записи: -3 = все упало, -2 = важный поток упал, -1 = нет потоков на запись, 0 = Стоп , 0.5 = запись останавливается, 1 = Идет запись",
	},
	[]string{"Room", "state"},
)

func init() {
	prometheus.MustRegister(recordStateGauge)
	prometheus.MustRegister(roomStateGauge)

}

func main() {

	// Создаём приложение
	app, err := BaseApp.InitBaseApp()
	if err != nil {
		log.Fatal(err)
	}

	router := gin.Default()
	// Подключаем маршруты
	api.SetupRoutes(router, app)
	// Эндпоинт для Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	// HTTP-сервер
	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(app.Constants.Port),
		Handler: router,
	}

	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	// kill (no params) by default sends syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown Server ...")
	app.ControlRuntime.Cancel()
	app.WG.Wait()
	if err := srv.Shutdown(app.ControlRuntime.CTX); err != nil {
		log.Println("Server Shutdown:", err)
	}

	log.Println("Server exiting")
}
