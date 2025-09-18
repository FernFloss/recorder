package VideoRTSP

import (
	general "RecoderAgent/internal/GeneralStructer"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/examples"
	"github.com/go-gst/go-gst/gst"
)

const recordType = "videoRTSP"

type VideoRTSP struct {
	statusChannel   chan general.MonitoringState
	repeatSignal    chan struct{}
	state           general.MonitoringState
	Name            string `json:"name"`
	Url             string `json:"url"`
	pipeline        *gst.Pipeline
	file            *os.File
	pathStored      string
	count           int
	stopSignal      bool
	recordIsRunning bool
	significant     bool
	stateMutex      sync.Mutex
}

func InitVideoRTSP(ctx context.Context, wg *sync.WaitGroup,
	startSignal, stopSignal <-chan interface{}, path, name, url string, sign bool) (general.Record, error) {

	//создаем файл, куда будем писать события для постобработки
	fileToWrite, err := general.CreateFile(path, name, recordType)
	if err != nil {
		return nil, err
	}

	//Создаем папку для хранения видео
	pathStoreVideo := path + "/" + name + "_" + recordType
	err = os.MkdirAll(pathStoreVideo, 0755)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания папки %s: %v", pathStoreVideo, err)
	}

	//Присваем нужные переменные?
	r := &VideoRTSP{
		statusChannel: make(chan general.MonitoringState),
		repeatSignal:  make(chan struct{}),
		state:         general.MonitoringState{State: "Initialized", Significant: sign, Errors: "nil"},
		Name:          name,
		Url:           url,
		file:          fileToWrite,
		pathStored:    pathStoreVideo,
		significant:   sign,
	}
	//Инициализации команды Gstreamer
	gst.Init(nil)
	orDone := general.Or(ctx, stopSignal)
	go r.recordingProccess(wg, startSignal, orDone)
	return r, nil
}

func (r *VideoRTSP) recordingProccess(wg *sync.WaitGroup,
	startSignal, done <-chan interface{}) {
	defer wg.Done()
	defer close(r.statusChannel)
	defer r.file.Close()
	var wgStream sync.WaitGroup

	//Запускаем мониторинг
	wgStream.Add(1)
	go r.runMonitoring(&wgStream, done)

	//инициализируем пайплайн заранее
	err := r.createPipeline()
	if err != nil {
		r.updateState("Error", err.Error())
	} else {
		r.updateState("Ready", "nil")
	}

	//ждем сигнала старта или выхода сразу
	select {
	case <-done:
		fmt.Printf("Зашли в первый дан with %s \n", r.Name)
		if err = r.pipeline.SetState(gst.StateNull); err != nil {
			fmt.Printf("ERROR with %s could not do gst.StateNull : %v\n", r.Name, err)
		}
		wgStream.Wait()
		return
	case <-startSignal:
		fmt.Printf("Получили сигнал старта %s \n", r.Name)
	}

	if err == nil {
		wgStream.Add(1)
		go r.startRunPipeline(&wgStream, done)
		fmt.Printf("Первый старт пайплайна %s \n", r.Name)
	}
	for {
		select {
		case <-done:
			close(r.repeatSignal)
			r.stopSignal = true
			fmt.Printf("Начали выходить %s \n", r.Name)
			ans := r.pipeline.SendEvent(gst.NewEOSEvent())
			if !ans {
				if err = r.pipeline.SetState(gst.StateNull); err != nil {
					fmt.Println("Error with "+r.Name+" stopping pipeline in Done:", err)
				}
			}
			wgStream.Wait()
			fmt.Printf("Выходим %s \n", r.Name)
			return
		case <-r.repeatSignal:
			fmt.Printf("Пришел сигнал репита %s \n", r.Name)
			wgStream.Add(1)
			r.createPipeline()
			go r.startRunPipeline(&wgStream, done)
		}
	}
}

func (r *VideoRTSP) startRunPipeline(wgStream *sync.WaitGroup, done <-chan interface{}) {
	defer wgStream.Done()
	fmt.Println(time.Now().Format("15:04:05.000"), " startRunPipeline by "+r.Name+" is started")
	examples.RunLoop(func(loop *glib.MainLoop) error {
		return r.runPipeline(loop)
	})
	fmt.Println(time.Now().Format("15:04:05.000"), " startRunPipeline by "+r.Name+" is finished")
	if !r.stopSignal {
		time.Sleep(general.RetryDelay)
		select {
		case <-done:
			fmt.Println("В горутине записи выходим из-за сигнала выхода")
			return
		default:
			fmt.Println("Пока не выходим")
		}
		if !r.stopSignal {
			select {
			case r.repeatSignal <- struct{}{}:
				return
			}
		}
		return
	}
}

func (r *VideoRTSP) runPipeline(loop *glib.MainLoop) error {
	bus := r.pipeline.GetPipelineBus()
	//Запускаем пайплайн
	r.pipeline.SetState(gst.StatePlaying)
	//Ставим шину для прослушки событий
	bus.AddWatch(func(msg *gst.Message) (cont bool) {
		// Assume we are continuing
		cont = true
		switch msg.Type() {
		case gst.MessageEOS:
			fmt.Println("Received EOS")
			r.updateState("EOS", "nil")
			loop.Quit()
		case gst.MessageError:
			err := msg.ParseError()
			r.updateState("Error", err.Error())
			fmt.Printf("[%s] ERROR RunPipe: %v by %s\n", time.Now().Format("15:04:05.000"), err, r.Name)
			fmt.Println(time.Now().Format("15:04:05.000"), " DEBUG RunPipe:", " by ", r.Name, err.DebugString())
			loop.Quit()
		}
		return
	})
	fmt.Println(time.Now().Format("15:04:05.000"), " RunPipe:", " by ", r.Name)
	loop.Run()
	fmt.Println(time.Now().Format("15:04:05.000"), " AFTER  RunPipe:", " by ", r.Name)
	// Stop the pipeline
	if err := r.pipeline.BlockSetState(gst.StateNull); err != nil {
		fmt.Println("Error with "+r.Name+" stopping pipeline:", err)
	}
	fmt.Println("DISABLE  BUS RunPipe:", " by ", r.Name)
	bus.RemoveWatch()
	if !r.recordIsRunning {
		filename := fmt.Sprintf("%s_%s_%d.mkv", r.Name, recordType, r.count)
		fmt.Printf("❌ удаляем файл, потому что ничего не положили в файла %s :\n", r.Name)
		os.Remove(r.pathStored + "/" + filename)
		r.count--
	}
	r.recordIsRunning = false

	return nil

}

func (r *VideoRTSP) runMonitoring(wgStream *sync.WaitGroup, done <-chan interface{}) {
	defer wgStream.Done()
	ticker := time.NewTicker(26 * time.Second)
	for {
		r.stateMutex.Lock()
		nowState := r.state
		r.stateMutex.Unlock()
		select {
		case <-done:
			return
		case r.statusChannel <- nowState:
		case <-ticker.C:
		}
	}
}

func (r *VideoRTSP) buildPipelineString() string {
	r.count++

	filename := fmt.Sprintf("%s_%s_%d.mkv", r.Name, recordType, r.count)

	pipeline := strings.Join([]string{
		"rtspsrc", "location=" + r.Url, "protocols=tcp", "latency=200", "drop-on-latency=true",
		"!", "queue", "!", "rtph264depay", "!", "h264parse", "!", "tee", "name=t",
		"t.", "!", "queue", "!", "matroskamux", "!", "filesink", "name=fsink",
		"location=" + r.pathStored + "/" + filename,
	}, " ")

	return pipeline
}

func (r *VideoRTSP) createPipeline() error {
	pipelineStr := r.buildPipelineString()
	var err error
	r.pipeline, err = gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		fmt.Printf("ERROR with %s : %v\n", r.Name, err)
		return err
	}

	sink, err := r.pipeline.GetElementByName("fsink")
	if err != nil {
		fmt.Printf("❌ fsink not found by %s: %v \n", r.Name, err)
		return err
	}

	sinkPad := sink.GetStaticPad("sink")
	sinkPad.AddProbe(gst.PadProbeTypeBuffer, func(pad *gst.Pad, info *gst.PadProbeInfo) gst.PadProbeReturn {
		//фикусируем время первого байта в sink
		timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
		fmt.Println("Запись в файл у " + r.Name + " в " + timestamp)
		r.recordIsRunning = true

		// логика записи таймстемпа в файл
		err = r.writeStatusFile(timestamp)
		if err != nil {
			fmt.Printf("❌ Could not write timestamp in file by %s: %v\n", r.Name, err)
		}
		// логика обновления состояния для мониторинга
		r.updateState("on", "nil")

		return gst.PadProbeRemove
	})
	return nil
}

func (r *VideoRTSP) updateState(state string, err string) {
	r.stateMutex.Lock()
	defer r.stateMutex.Unlock()
	r.state = general.MonitoringState{State: state, Errors: err, Significant: r.significant}
}

func (r *VideoRTSP) writeStatusFile(timestamp string) error {
	filename := fmt.Sprintf("%s_%s_%d.mkv", r.Name, recordType, r.count)
	s := general.Status{Status: filename, Timestamp: timestamp}
	jsonData, err := json.Marshal(s)
	if err != nil {
		fmt.Println("Error marshaling:", err)
		return err
	}
	_, err = fmt.Fprintln(r.file, string(jsonData))
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}
	return nil
}

func (r *VideoRTSP) SendMonitoringInfo() <-chan general.MonitoringState {
	return r.statusChannel
}
