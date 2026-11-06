package bench

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ascendingheavens/falcon"
	falconmw "github.com/ascendingheavens/falcon/middleware"
	"github.com/gin-gonic/gin"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
)

// envelope mirrors falcon.Response, so all three frameworks encode the same JSON.
type envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	Code    int    `json:"code"`
}

// issue is what the realistic handlers respond with.
type issue struct {
	Number string `json:"number"`
	State  string `json:"state"`
	Page   string `json:"page"`
}

const issuesTarget = "/repos/acme/falcon/issues/42?state=open&page=2"

// realisticApps build what a real API looks like: a full route table, logging and panic
// recovery, and a handler that reads params and query values and responds with JSON.
// Logs are discarded, so that this measures formatting them and not the speed of a disk.
// Falcon and Echo write the same fields, but Gin writes its own default line, so they aren't
// doing identical logging work. Building without logging takes that out of the comparison.
var realisticApps = []struct {
	name  string
	build func(logging bool) http.Handler
}{
	{"falcon", func(logging bool) http.Handler {
		app := falcon.New()
		if logging {
			app.Use(falconmw.Logger())
		}
		app.Use(falconmw.Recovery())
		for _, r := range githubRoutes {
			app.Handle(r.method, r.path, func(c *falcon.Context) *falcon.Response {
				return c.OK("Issue found", issue{c.Param("number"), c.Query("state"), c.Query("page")})
			})
		}
		return app
	}},
	{"gin", func(logging bool) http.Handler {
		gin.SetMode(gin.ReleaseMode)
		app := gin.New()
		if logging {
			app.Use(gin.LoggerWithWriter(io.Discard))
		}
		app.Use(gin.Recovery())
		for _, r := range githubRoutes {
			app.Handle(r.method, r.path, func(c *gin.Context) {
				c.JSON(200, envelope{
					Success: true,
					Message: "Issue found",
					Details: issue{c.Param("number"), c.Query("state"), c.Query("page")},
					Code:    200,
				})
			})
		}
		return app
	}},
	{"echo", func(logging bool) http.Handler {
		app := echo.New()
		if logging {
			app.Use(echomw.RequestLoggerWithConfig(echomw.RequestLoggerConfig{
				LogMethod:  true,
				LogURIPath: true,
				LogStatus:  true,
				LogLatency: true,
				LogValuesFunc: func(c echo.Context, v echomw.RequestLoggerValues) error {
					// It has no line of its own, so this writes the same one as Falcon's Logger.
					_, err := fmt.Fprintf(io.Discard, "[%s] %s %q %d (%v)\n",
						time.Now().Format(time.RFC3339), v.Method, v.URIPath, v.Status, v.Latency)
					return err
				},
			}))
		}
		app.Use(echomw.Recover())
		for _, r := range githubRoutes {
			app.Add(r.method, r.path, func(c echo.Context) error {
				return c.JSON(200, envelope{
					Success: true,
					Message: "Issue found",
					Details: issue{c.Param("number"), c.QueryParam("state"), c.QueryParam("page")},
					Code:    200,
				})
			})
		}
		return app
	}},
}
