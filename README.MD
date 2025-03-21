# Установка зависимостей
Установить gstreamer -> прописать его в путь в constant.json
Установить FFMPEG -> прописать его путь в constant.json
Для установки зависимостей

```PowerShell
go mod tidy 
```

# Эндпоинты

### GET /status
🔹 Возвращает список потоков с их состоянием и ошибками (если есть).

Пример:
```json
{
  "records": [
    {
      "name": "V1",
      "url": "rtsp://172.18.191.52:554/1/h264major",
      "state": "on",
      "error": ""
    },
    {
      "name": "Audio",
      "url": "rtsp://172.18.191.61/stream0",
      "state": "error",
      "error": "Connection lost"
    }
  ]
}
```

### POST /config

Загружает новую конфигурацию потоков.
Пример запроса: 

```bash
curl -X POST http://localhost:5000/config \
     -H "Content-Type: application/json" \
     -d '{
           "room": "Room1",
           "videos": [
             {
               "type": "video",
               "name": "V1",
               "url": "rtsp://172.18.191.52:554/1/h264major"
             },
             {
               "type": "video",
               "name": "V2",
               "url": "rtsp://172.18.191.53:554/Streaming/Channels/1"
             }
           ],
           "audios": [
             {
               "type": "audio",
               "name": "Audio",
               "url": "rtsp://172.18.191.61/stream0"
             }
           ],
           "destination": "./videos"
         }'
```

### GET /start

 Запускает запись всех потоков.

### GET /stop

Останавливает запись всех потоков.

### GET /configFromFile

Загружает конфигурацию из серверного файла config.json

### GET /metrics

Возвращает метрики о состоянии потоков.


# Постобработка
Для постобработки и синхронизации записанного материала необходимо запустить скрипт `editor.go`.

```PowerShell
go build editor.go
./editor <InputDir> <OutputDir>
```
