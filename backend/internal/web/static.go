package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var embeddedDist embed.FS

// SetupStaticRoutes wires embedded frontend static assets.
func SetupStaticRoutes(router *gin.Engine) {
	distFS, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic("embedded frontend dist unavailable: " + err.Error())
	}

	staticServer := http.FileServer(http.FS(distFS))
	serveIndex := func(c *gin.Context) {
		c.FileFromFS("index.html", http.FS(distFS))
	}

	router.GET("/", gin.WrapH(staticServer))
	router.HEAD("/", gin.WrapH(staticServer))
	router.GET("/index.html", gin.WrapH(staticServer))
	router.HEAD("/index.html", gin.WrapH(staticServer))
	router.GET("/assets/*filepath", gin.WrapH(staticServer))
	router.HEAD("/assets/*filepath", gin.WrapH(staticServer))

	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if strings.HasPrefix(c.Request.URL.Path, "/api/") ||
			c.Request.URL.Path == "/health" ||
			c.Request.URL.Path == "/ready" ||
			c.Request.URL.Path == "/metrics" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		serveIndex(c)
	})
}
