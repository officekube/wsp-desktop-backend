package workspaceEngine

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// Route is the information for every URI.
type Route struct {
	// Name is the name of this Route.
	Name string
	// Method is the string for the HTTP method. ex) GET, POST etc..
	Method string
	// Pattern is the pattern of the URI.
	Pattern string
	// HandlerFunc is the handler function of this route.
	HandlerFunc gin.HandlerFunc
}

// Routes is the list of the generated Route.
type Routes []Route

// NewEngine returns a new engine.
func NewEngine() *gin.Engine {
	engine := gin.Default()
	path, _ := Util.GetEngineInstallationPath()
	engine.LoadHTMLGlob(filepath.Join(path, "templates", "*"))

	for _, route := range routes {
		switch route.Method {
		case http.MethodGet:
			engine.GET(route.Pattern, route.HandlerFunc)
		case http.MethodPost:
			engine.POST(route.Pattern, route.HandlerFunc)
		case http.MethodPut:
			engine.PUT(route.Pattern, route.HandlerFunc)
		case http.MethodPatch:
			engine.PATCH(route.Pattern, route.HandlerFunc)
		case http.MethodDelete:
			engine.DELETE(route.Pattern, route.HandlerFunc)
		}
	}

	return engine
}

// Index is the index handler.
func Index(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Title":       "Welcome to the Workspace",
		"Description": "Enjoy the Most Productive Environment in the World!",
	})
}

var routes = Routes{
	{
		"Index",
		http.MethodGet,
		"/",
		Index,
	},
}
