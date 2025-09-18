package api

import (
	"RecoderAgent/api/handlers"
	BaseApp "RecoderAgent/internal/BaseApp"
	"fmt"

	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Println("CORS middleware triggered for:", c.Request.Method, c.Request.URL.Path) // 👈 Лог

		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// SetupRoutes настраивает все HTTP-маршруты
func SetupRoutes(r *gin.Engine, app *BaseApp.App) {
	r.Use(CORS()) // ← ЭТО ОБЯЗАТЕЛЬНО СДЕЛАТЬ ЗДЕСЬ!
	// Группа для управления записью
	recording := r.Group("/")

	{
		recording.POST("/config", handlers.ApplyConfigHandler(app))
		recording.POST("/start", handlers.StartRecordHandler(app))
		recording.POST("/stop", handlers.StopRecordHandler(app))
	}

	// Раздача статических файлов из папки static.
	// r.Static("/static", "./static")
	// Редирект с "/" на "/static/index.html"
	// r.GET("/", func(c *gin.Context) {
	// 	c.Redirect(http.StatusMovedPermanently, "/static/index.html")
	// })
}
