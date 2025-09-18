// api/handlers/tasks.go
package handlers

import (
	"errors"
	"net/http"

	BaseApp "RecoderAgent/internal/BaseApp"

	"github.com/gin-gonic/gin"
)

// StartRecordHandler получает конфигурацию и загружает в память
// @Summary		Запустить запись
// @Description	Запускает запись для уже загруженных потоков. Автоматическое восстановление при обрыве.
//
//	@Tags			recording
//	@Accept			json
//	@Produce		json
//
// @Success 200 {object} map[string]string "message: запись запущена"
//
//	@Failure		400		{object}	map[string]string		"error"
//	@Failure		500		{object}	map[string]string		"error"
//	@Router			/start [post]
func StartRecordHandler(app *BaseApp.App) gin.HandlerFunc {
	return func(c *gin.Context) {

		err := app.StartRecords()
		//проверяем ошибки со стороны клиента
		if errors.Is(err, BaseApp.ErrAlreadyRecording) ||
			errors.Is(err, BaseApp.ErrRecordsIsEmpty) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		//остальные ошибки сервера
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Запись запущена",
		})
	}
}
