package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StatusRecord описывает запись из JSON файла
type StatusRecord struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// Segment описывает сегмент записи (файл или промежуток заполнителя)
type Segment struct {
	Start    time.Time     // время старта сегмента
	Stop     time.Time     // время окончания сегмента
	File     string        // путь к файлу (если пусто, это заполнитель)
	IsGap    bool          // true – сегмент-заполнитель
	Duration time.Duration // длительность сегмента
}

// getDuration получает длительность файла через gst-discoverer-1.0
func getDuration(filePath string) (time.Duration, error) {
	fmt.Printf("[DEBUG] Получение длительности файла: %s\n", filePath)
	cmd := exec.Command("gst-discoverer-1.0", filePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[ERROR] gst-discoverer error: %v, output: %s\n", err, string(out))
		return 0, err
	}

	fmt.Printf("[DEBUG] Вывод gst-discoverer: %s\n", string(out))

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Duration:") {
			durationStr := strings.TrimSpace(strings.TrimPrefix(line, "Duration:"))
			fmt.Printf("[DEBUG] Найдена длительность: %s\n", durationStr)
			d, err := parseGstDuration(durationStr)
			if err != nil {
				fmt.Printf("[ERROR] Ошибка парсинга длительности: %v\n", err)
				return 0, err
			}
			return d, nil
		}
	}
	fmt.Println("[ERROR] Длительность не найдена в выводе gst-discoverer")
	return 0, fmt.Errorf("длительность не найдена в выводе gst-discoverer")
}

// parseGstDuration парсит строку вида "0:00:05.123456789" в time.Duration
func parseGstDuration(s string) (time.Duration, error) {
	fmt.Printf("[DEBUG] Парсинг длительности: %s\n", s)
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("неверный формат длительности: %s", s)
	}
	h, err := time.ParseDuration(parts[0] + "h")
	if err != nil {
		return 0, err
	}
	m, err := time.ParseDuration(parts[1] + "m")
	if err != nil {
		return 0, err
	}
	sec, err := time.ParseDuration(parts[2] + "s")
	if err != nil {
		return 0, err
	}
	return h + m + sec, nil
}

// parseTimestamp парсит строку времени из JSON
func parseTimestamp(ts string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, ts)
}

// formatTimestampForFilename преобразует время в строку для имени файла
func formatTimestampForFilename(t time.Time) string {
	return t.Format("20060102_150405")
}

// generateGap создает заглушку для разрыва: для video – черный экран, для audio – тишина
func generateGap(duration time.Duration, streamType, outputFolder, streamName string, gapIndex int) (string, error) {
	gapFile := filepath.Join(outputFolder, fmt.Sprintf("%s_%s_gap_%d.mkv", streamName, streamType, gapIndex))
	durationStr := fmt.Sprintf("%.3f", duration.Seconds())

	var cmd *exec.Cmd
	if streamType == "video" {
		// Черный экран (разрешение можно менять по необходимости)
		cmd = exec.Command("ffmpeg", "-y", "-f", "lavfi",
			"-i", fmt.Sprintf("color=c=black:s=1280x720:d=%s", durationStr),
			"-c:v", "libx264", gapFile)
	} else {
		// Тишина
		cmd = exec.Command("ffmpeg", "-y", "-f", "lavfi",
			"-i", "anullsrc=r=48000:cl=stereo", "-t", durationStr,
			"-c:a", "aac", gapFile)
	}

	fmt.Printf("[DEBUG] Генерация заглушки: %s (длительность: %s)\n", gapFile, durationStr)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ошибка создания заглушки: %v", err)
	}
	return gapFile, nil
}

// computeGlobalBoundsAll сканирует все JSON-файлы (video и audio) и вычисляет глобальный старт и стоп
func computeGlobalBoundsAll(inputFolder string) (time.Time, time.Time, error) {
	files, err := ioutil.ReadDir(inputFolder)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var globalStart, globalStop time.Time
	first := true

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		// Рассматриваем только JSON-файлы для video или audio
		if strings.HasSuffix(name, "__video__status.json") || strings.HasSuffix(name, "__audio__status.json") {
			jsonPath := filepath.Join(inputFolder, name)
			data, err := ioutil.ReadFile(jsonPath)
			if err != nil {
				continue
			}
			var records []StatusRecord
			if err := json.Unmarshal(data, &records); err != nil {
				continue
			}
			// Определяем имя потока
			parts := strings.Split(name, "__")
			if len(parts) < 2 {
				continue
			}
			streamName := parts[0]

			// Для каждой записи с "started"
			for _, rec := range records {
				// Проверяем, начинается ли статус с "started_"
				if !strings.HasPrefix(rec.Status, "started_") {
					continue
				}
			
				// Извлекаем имя файла из статуса
				fileName := strings.TrimPrefix(rec.Status, "started_")
			
				// Парсим временную метку
				t, err := parseTimestamp(rec.Timestamp)
				if err != nil {
					continue
				}
			
				// Формируем путь до файла (внутри папки потока)
				filePath := filepath.Join(inputFolder, streamName, fileName)
			
				// Получаем длительность видео
				dur, err := getDuration(filePath)
				if err != nil {
					fmt.Printf("[WARN] Не удалось получить длительность для файла %s: %v\n", filePath, err)
					continue
				}
			
				// Вычисляем время окончания
				stopTime := t.Add(dur)
			
				// Обновляем глобальные значения начала и конца
				if first {
					globalStart = t
					globalStop = stopTime
					first = false
				} else {
					if t.Before(globalStart) {
						globalStart = t
					}
					if stopTime.After(globalStop) {
						globalStop = stopTime
					}
				}
			}
		}
	}
	if first {
		return time.Time{}, time.Time{}, fmt.Errorf("нет валидных таймстемпов")
	}
	return globalStart, globalStop, nil
}

// processStream обрабатывает один поток (video или audio)
func processStream(streamName, streamType, inputFolder, outputFolder string, globalStart, globalStop time.Time) error {
	fmt.Printf("[INFO] Начало обработки потока: %s (%s)\n", streamName, streamType)

	jsonFileName := fmt.Sprintf("%s__%s__status.json", streamName, streamType)
	jsonPath := filepath.Join(inputFolder, jsonFileName)
	fmt.Printf("[DEBUG] Читаем JSON файл: %s\n", jsonPath)

	data, err := ioutil.ReadFile(jsonPath)
	if err != nil {
		fmt.Printf("[ERROR] Ошибка чтения файла %s: %v\n", jsonPath, err)
		return err
	}

	var records []StatusRecord
	if err := json.Unmarshal(data, &records); err != nil {
		fmt.Printf("[ERROR] Ошибка парсинга JSON %s: %v\n", jsonPath, err)
		return err
	}
	fmt.Printf("[DEBUG] Загруженные записи JSON: %+v\n", records)

	var segments []Segment
	streamFolder := filepath.Join(inputFolder, streamName)

	for _, rec := range records {
		// Проверка, начинается ли статус с "started_"
		if !strings.HasPrefix(rec.Status, "started_") {
			continue
		}
	
		// Извлекаем имя файла из статуса
		fileName := strings.TrimPrefix(rec.Status, "started_")
		filePath := filepath.Join(streamFolder, fileName)
		fmt.Printf("[DEBUG] Ожидаемый файл: %s\n", filePath)
	
		// Парсинг времени старта
		startTime, err := parseTimestamp(rec.Timestamp)
		if err != nil {
			fmt.Printf("[ERROR] Ошибка парсинга timestamp %s: %v\n", rec.Timestamp, err)
			continue
		}
	
		// Получение длительности видеофрагмента
		dur, err := getDuration(filePath)
		if err != nil {
			fmt.Printf("[ERROR] Ошибка получения длительности файла %s: %v\n", filePath, err)
			continue
		}
	
		// Вычисляем время окончания
		stopTime := startTime.Add(dur)
	
		fmt.Printf("[DEBUG] Сегмент: Start=%s, Stop=%s, Duration=%s, File=%s\n",
			startTime.Format(time.RFC3339Nano), stopTime.Format(time.RFC3339Nano), dur, filePath)
	
		// Добавляем сегмент в список
		segments = append(segments, Segment{
			Start:    startTime,
			Stop:     stopTime,
			File:     filePath,
			IsGap:    false,
			Duration: dur,
		})
	}

	if len(segments) == 0 {
		return fmt.Errorf("нет валидных сегментов для потока %s", streamName)
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Start.Before(segments[j].Start)
	})
	fmt.Printf("[DEBUG] Отсортированные сегменты: %+v\n", segments)

	// Сохраним информацию о разрывах в файл gaps_<streamName>_<streamType>.txt
	gapsFilePath := filepath.Join(outputFolder, fmt.Sprintf("gaps_%s_%s.txt", streamName, streamType))
	gapsFile, err := os.Create(gapsFilePath)
	if err != nil {
		return fmt.Errorf("ошибка создания файла для разрывов: %v", err)
	}
	defer gapsFile.Close()

	var finalSegments []Segment
	gapIndex := 0

	// Если начало первого сегмента позже глобального старта, вставляем заглушку (если длительность ≥ 1 сек).
	if segments[0].Start.After(globalStart) {
		gapDur := segments[0].Start.Sub(globalStart)
		if gapDur >= time.Millisecond {
			fmt.Printf("[INFO] Начало потока (%s) позже глобального старта (%s), вставка заглушки длительностью %s\n",
				segments[0].Start.Format(time.RFC3339Nano), globalStart.Format(time.RFC3339Nano), gapDur)
			gapFile, err := generateGap(gapDur, streamType, outputFolder, streamName, gapIndex)
			if err != nil {
				return err
			}
			gapSegment := Segment{
				Start:    globalStart,
				Stop:     segments[0].Start,
				File:     gapFile,
				IsGap:    true,
				Duration: gapDur,
			}
			finalSegments = append(finalSegments, gapSegment)
			gapIndex++
		} else {
			fmt.Printf("[INFO] Заглушка в начале пропущена, длительность разрыва (%s) меньше миллисекунды\n", gapDur)
		}
	}

	// Добавляем первый сегмент
	finalSegments = append(finalSegments, segments[0])

	// Проходим по парам сегментов
	for i := 0; i < len(segments)-1; i++ {
		current := segments[i]
		next := segments[i+1]
		if next.Start.After(current.Stop) {
			gapDur := next.Start.Sub(current.Stop)
			fmt.Printf("[INFO] Обнаружен разрыв между %s и %s, длительность %s\n",
				current.Stop.Format(time.RFC3339Nano), next.Start.Format(time.RFC3339Nano), gapDur)
			// Записываем информацию о разрыве в файл
			gapInfo := fmt.Sprintf("Gap %d: Start=%s, Stop=%s, Duration=%s\n",
				gapIndex, current.Stop.Format(time.RFC3339Nano), next.Start.Format(time.RFC3339Nano), gapDur)
			gapsFile.WriteString(gapInfo)
			// Если длительность разрыва меньше миллисекунды, пропускаем генерацию заглушки
			if gapDur >= time.Millisecond {
				gapFile, err := generateGap(gapDur, streamType, outputFolder, streamName, gapIndex)
				if err != nil {
					return err
				}
				gapSegment := Segment{
					Start:    current.Stop,
					Stop:     next.Start,
					File:     gapFile,
					IsGap:    true,
					Duration: gapDur,
				}
				finalSegments = append(finalSegments, gapSegment)
				gapIndex++
			} else {
				fmt.Printf("[INFO] Заглушка между сегментами пропущена, разрыв (%s) меньше миллисекунды\n", gapDur)
			}
		}
		finalSegments = append(finalSegments, next)
	}

	// Если конец последнего сегмента раньше глобального стопа, добавляем заглушку (если длительность ≥ 1 сек).
	last := segments[len(segments)-1]
	if last.Stop.Before(globalStop) {
		gapDur := globalStop.Sub(last.Stop)
		if gapDur >= time.Millisecond {
			fmt.Printf("[INFO] Конец потока (%s) раньше глобального стопа (%s), вставка заглушки длительностью %s\n",
				last.Stop.Format(time.RFC3339Nano), globalStop.Format(time.RFC3339Nano), gapDur)
			gapFile, err := generateGap(gapDur, streamType, outputFolder, streamName, gapIndex)
			if err != nil {
				return err
			}
			gapSegment := Segment{
				Start:    last.Stop,
				Stop:     globalStop,
				File:     gapFile,
				IsGap:    true,
				Duration: gapDur,
			}
			finalSegments = append(finalSegments, gapSegment)
		} else {
			fmt.Printf("[INFO] Заглушка в конце пропущена, разрыв (%s) меньше миллисекунды\n", gapDur)
		}
	}

	fmt.Printf("[DEBUG] Итоговый список сегментов (с заглушками): %+v\n", finalSegments)

	// Создаем файл списка для ffmpeg (concat демаксера)
	listFilePath := filepath.Join(outputFolder, fmt.Sprintf("%s_%s_list.txt", streamName, streamType))
	listFile, err := os.Create(listFilePath)
	if err != nil {
		return fmt.Errorf("ошибка создания файла списка: %v", err)
	}
	defer listFile.Close()

	for _, seg := range finalSegments {
		absPath, err := filepath.Abs(seg.File)
		if err != nil {
			return fmt.Errorf("не удалось получить абсолютный путь для %s: %v", seg.File, err)
		}
		listFile.WriteString(fmt.Sprintf("file '%s'\n", absPath))
	}
	listFile.Sync()
	fmt.Printf("[DEBUG] Файл списка для конкатенации: %s\n", listFilePath)

	// Склеиваем сегменты в один итоговый файл
	outputFile := filepath.Join(outputFolder, fmt.Sprintf("%s_%s_final.mkv", streamName, streamType))
	concatCmd := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0",
		"-i", listFilePath, "-c", "copy", outputFile)
	concatCmd.Stdout = os.Stdout
	concatCmd.Stderr = os.Stderr
	if err := concatCmd.Run(); err != nil {
		return fmt.Errorf("ошибка конкатенации: %v", err)
	}

	fmt.Printf("[INFO] Поток %s (%s) успешно обработан, результат: %s\n", streamName, streamType, outputFile)
	return nil
}

// mergeAudioToVideo накладывает аудио из audioFile на videoFile.
// Результат сохраняется в mergedFile.
func mergeAudioToVideo(videoFile, audioFile, mergedFile string) error {
	fmt.Printf("[INFO] Наложение аудио %s на видео %s\n", audioFile, videoFile)
	cmd := exec.Command("ffmpeg", "-y",
		"-i", videoFile,
		"-i", audioFile,
		"-c", "copy",
		"-map", "0:v:0",
		"-map", "1:a:0",
		mergedFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Использование: script <input_folder> <output_folder>")
		os.Exit(1)
	}
	inputFolder := os.Args[1]
	outputFolder := os.Args[2]

	// Создаем папку для результатов, если ее нет
	if err := os.MkdirAll(outputFolder, 0755); err != nil {
		fmt.Printf("[ERROR] Не удалось создать папку для результатов: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[INFO] Запуск обработки. Входная папка: %s, Выходная папка: %s\n", inputFolder, outputFolder)

	files, err := ioutil.ReadDir(inputFolder)
	if err != nil {
		fmt.Printf("[ERROR] Ошибка чтения каталога %s: %v\n", inputFolder, err)
		os.Exit(1)
	}

	// Вычисляем глобальный старт и глобальный стоп среди всех потоков
	globalStart, globalStop, err := computeGlobalBoundsAll(inputFolder)
	if err != nil {
		fmt.Printf("[WARN] Не удалось вычислить глобальные границы: %v\n", err)
	} else {
		fmt.Printf("[INFO] Глобальный старт: %s\n", globalStart.Format(time.RFC3339Nano))
		fmt.Printf("[INFO] Глобальный стоп: %s\n", globalStop.Format(time.RFC3339Nano))
	}

	// Перебираем ВСЕ существующие JSON-файлы
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()

		// Проверяем, является ли файл статусным JSON для video или audio
		if strings.HasSuffix(name, "__video__status.json") {
			streamName := strings.Split(name, "__")[0]
			fmt.Printf("[INFO] Обнаружен JSON видео: %s (поток: %s)\n", name, streamName)
			_ = processStream(streamName, "video", inputFolder, outputFolder, globalStart, globalStop)
		} else if strings.HasSuffix(name, "__audio__status.json") {
			streamName := strings.Split(name, "__")[0]
			fmt.Printf("[INFO] Обнаружен JSON аудио: %s (поток: %s)\n", name, streamName)
			_ = processStream(streamName, "audio", inputFolder, outputFolder, globalStart, globalStop)
		}
	}

	// Если обнаружен итоговый аудиофайл, накладываем его на все итоговые видео
	var audioFinal string
	files, _ = ioutil.ReadDir(outputFolder)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), "_audio_final.mkv") {
			audioFinal = filepath.Join(outputFolder, f.Name())
			break
		}
	}
	if audioFinal != "" {
		fmt.Printf("[INFO] Найден итоговый аудиофайл: %s\n", audioFinal)
		// Для каждого итогового видео накладываем аудио
		for _, f := range files {
			if strings.HasSuffix(f.Name(), "_video_final.mkv") {
				videoFile := filepath.Join(outputFolder, f.Name())
				mergedFile := strings.Replace(videoFile, "_video_final.mkv", "_final.mkv", 1)
				if err := mergeAudioToVideo(videoFile, audioFinal, mergedFile); err != nil {
					fmt.Printf("[ERROR] Ошибка наложения аудио на видео %s: %v\n", videoFile, err)
					continue
				}
				// Удаляем исходное видео без аудио
				os.Remove(videoFile)
				fmt.Printf("[INFO] Финальный файл с аудио: %s\n", mergedFile)
			}
		}
		// Удаляем итоговый аудиофайл, чтобы в result остались только финальные видео
		os.Remove(audioFinal)
	} else {
		fmt.Println("[WARN] Итоговый аудиофайл не найден, накладывать аудио не удалось")
	}

	// Удаляем временные файлы
	patterns := []string{
		"gaps_*_video.txt",
		"*_video_list.txt",
		"*_audio_list.txt",
		"gaps_*_audio.txt",
		"*_video_gap_*.mkv",
		"*_audio_gap_*.mkv",
	}
	for _, pattern := range patterns {
		fullPattern := filepath.Join(outputFolder, pattern)
		files, err := filepath.Glob(fullPattern)
		if err != nil {
			fmt.Printf("Ошибка поиска по шаблону %s: %v\n", fullPattern, err)
			continue
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				fmt.Printf("Ошибка удаления файла %s: %v\n", f, err)
			} else {
				fmt.Printf("[INFO] Удален временный файл: %s\n", f)
			}
		}
	}
	
}
