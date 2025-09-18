// api/handlers/tasks.go
package handlers

import (
	"errors"
	"net/http"

	BaseApp "RecoderAgent/internal/BaseApp"

	"github.com/gin-gonic/gin"
)

// StopRecordHandler останавливает потоки и очищает их в памяти
//
//	@Summary		Остановить запись потоков, загруженных в память и очистить ее
//
// @Description	Останавливает активную запись и освобождает ресурсы. Поддерживает graceful shutdown.
//
//	@Tags			recording
//	@Accept			json
//	@Produce		json
//
// @Success 200 {object} map[string]string "message: запись остановлена"
//
//	@Failure		400		{object}	map[string]string		"error"
//	@Failure		500		{object}	map[string]string		"error"
//	@Router			/stop [post]
func StopRecordHandler(app *BaseApp.App) gin.HandlerFunc {
	return func(c *gin.Context) {

		err := app.StopRecords()
		//проверяем ошибки со стороны клиента
		if errors.Is(err, BaseApp.ErrRecordsIsEmpty) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		//остальные ошибки сервера
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Запись остановлена",
		})
	}
}
