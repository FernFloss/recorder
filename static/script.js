let videoCount = 0;

// document.addEventListener("DOMContentLoaded", function() {
//     //fetchStatusMetrics();
//     //setInterval(fetchStatusMetrics, 1000); // Обновление статуса раз в 1 секунд
//     setInterval(fetchStatus, 1000);
// });

// function fetchStatus() {
//     fetch("/status")
//         .then(response => response.json())
//         .then(data => {
//             const statusElement = document.getElementById("roomStatus");
//             statusElement.textContent = data.status;
//             // Опционально: добавить стили в зависимости от статуса
//         statusElement.className = 'status-' + data.status.toLowerCase().replace(' ', '-');
//         })
//         .catch(err => console.error("Ошибка загрузки статусов:", err));
// }

// function fetchStatusMetrics() {
//     fetch("/statusMetrics")
//         .then(response => response.json())
//         .then(data => {
//             const table = document.getElementById("statusTable");
//             table.innerHTML = ""; // Очищаем таблицу

//             data.records.forEach(record => {
//                 const row = document.createElement("tr");
//                 row.innerHTML = `
//                     <td>${record.name}</td>
//                     <td>${record.url}</td>
//                     <td>${record.state === "on" ? "🟢 Работает" : (record.state === "error" ? "🔴 Ошибка" : "⚫ Остановлено")}</td>
//                     <td>${record.error || "—"}</td>
//                 `;
//                 if (record.state === "error") {
//                     row.style.backgroundColor = "#ffcccc"; // Красный фон при ошибке
//                 }
//                 table.appendChild(row);
//             });
//         })
//         .catch(err => console.error("Ошибка загрузки статусов:", err));
// }

function validateUrl(url) {
    return /^(rtsp|srt):\/\//i.test(url);
}

function addStream(type) {
    if (videoCount >= 5) {
        alert("Можно добавить не более 5 видеопотоков!");
        return;
    }

    videoCount++;
    const container = document.getElementById("videoStreams");
    const div = document.createElement("div");
    div.setAttribute("id", `video${videoCount}`);

   div.innerHTML = `
        <label for="video${videoCount}_name">Имя:</label>
        <input type="text" id="video${videoCount}_name">
        <label for="video${videoCount}_url">Видео URL:</label>
        <input type="text" id="video${videoCount}_url" class="long-input">
        <label>
            <input type="checkbox" id="video${videoCount}_significant"> Важный поток
        </label>
        <button type="button" onclick="removeVideoStream(${videoCount})">Удалить</button>
        <span class="error" id="video${videoCount}Error"></span>
    `;
    
    container.appendChild(div);
}

function removeVideoStream(id) {
    const element = document.getElementById(`video${id}`);
    if (element) {
        element.remove();
        videoCount--;
    }
}

function validateStream(index, type) {
    const urlField = document.getElementById(`${type}${index}_url`);
    const errorElem = document.getElementById(`${type}${index}Error`);

    errorElem.innerText = "";
    const url = urlField.value.trim();

    if (url && !validateUrl(url)) {
        errorElem.innerText = "Неверный формат URL (должен начинаться с rtsp:// или srt://)";
        return false;
    }
    return true;
}

function submitConfig() {
    const room = document.getElementById("room").value.trim();
    const destination = document.getElementById("destination").value.trim() || "./videos";

    if (!room) {
        alert("Название комнаты обязательно");
        return;
    }

    const videos = [];
    document.querySelectorAll("#videoStreams div").forEach(div => {
        const name = div.querySelector("input[id$='_name']").value.trim();
        const url = div.querySelector("input[id$='_url']").value.trim();
        const significantInput = div.querySelector("input[id$='_significant']");
        const significant = significantInput.checked; // true/fals
        
        if (url && name) {
            videos.push({ name, url, significant});
        }
    });

    if (videos.length < 1) {
        alert("Необходимо указать хотя бы один видео поток");
        return;
    }

    const audioName = document.getElementById("audio_name").value.trim();
    const audioUrl = document.getElementById("audio_url").value.trim();
    const audioSignificant = document.getElementById("audio_significant").checked;
    if (!audioName || !audioUrl) {
        alert("Необходимо указать аудио поток");
        return;
    }

    const config = {
        room,
        destination,
        videos,
        audios: [{ name: audioName, url: audioUrl,
            significant: audioSignificant }],
        };

    fetch("/config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(config)
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            alert(data.error);
            return;
        } else {
            alert(data.message);
        }
    })
    .catch(err => alert("Ошибка отправки конфигурации: " + err));
}

document.getElementById("downloadFromFileButton").addEventListener("click", () => {
    fetch("/configFromFile")
        .then(response => response.json())
        .then(data => {
            if (data.error) {
                alert(data.error);
                return;
            } else {
                alert(data.message);
            }
        })
        .catch(err => alert("Ошибка загрузки конфигурации: " + err));
});

document.getElementById("startButton").addEventListener("click", () => {
    fetch("/start", {
        method: "POST",
        headers: { "Content-Type": "application/json" }
    })
        .then(response => response.json())
        .then(data => {
            if (data.error) {
                alert(data.error);
                return;
            } else {
                alert(data.message);
            }
        })
        .catch(err => alert("Ошибка сети: " + err));
});

document.getElementById("stopButton").addEventListener("click", () => {
    fetch("/stop", {
        method: "POST",
        headers: { "Content-Type": "application/json" }
    })
        .then(response => response.json())
        .then(data => {
            if (data.error) {
                alert(data.error);
                return;
            } else {
                alert(data.message);
            }
        })
        .catch(err => alert("Ошибка остановки: " + err));
});

