// api/handlers/tasks.go
package handlers

import (
	"errors"
	"net/http"

	BaseApp "RecoderAgent/internal/BaseApp"
	DTO "RecoderAgent/internal/dto"

	"github.com/gin-gonic/gin"
)

// ApplyConfigHandler получает конфигурацию и загружает в память
//
//	@Summary		Загрузить конфигурацию
//	@Description	Принимает JSON-объект конфигурации, сохраняет его и инициализирует в приложение
//	@Tags			config
//	@Accept			json
//	@Produce		json
//	@Param			config	body		BaseApp.Config	true	"Конфигурация приложения"
//	@Success		200		{object}	DTO.ApplyConfigResponse	"message и config"
//	@Failure		400		{object}	map[string]string		"error"
//	@Failure		500		{object}	map[string]string		"error"
//	@Router			/config [post]
func ApplyConfigHandler(app *BaseApp.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config BaseApp.Config
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := app.LoadAndInitConfig(config)
		//проверяем ошибки со стороны клиента
		if errors.Is(err, BaseApp.ErrAlreadyRecording) ||
			errors.Is(err, BaseApp.ErrConfigAlreadyLoaded) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		//остальные ошибки сервера
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := DTO.ApplyConfigResponse{
			Message: "Конфигурация получена, сохранена и загружена в память",
			Config:  config,
		}
		c.JSON(http.StatusOK, response)
	}
}
