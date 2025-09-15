package server

import (
	"html/template"
	"net/http"
	"sync"
)

// TemplateRenderer manages HTML templates for rendering in Falcon.
// It supports thread-safe access and optional development mode for live reloading.
type TemplateRenderer struct {
	templates *template.Template
	funcs     template.FuncMap
	mu        sync.RWMutex
	pattern   string
	devMode   bool
}

// NewTemplateRenderer initializes a TemplateRenderer that parses templates
// from the given glob pattern. The devMode flag enables live reloading of
// templates on each render (useful during development).
// Example: NewTemplateRenderer("views/*.html", true, funcs)
func NewTemplateRenderer(pattern string, devMode bool, funcs template.FuncMap) *TemplateRenderer {
	tr := &TemplateRenderer{
		funcs:   funcs,
		pattern: pattern,
		devMode: devMode,
	}
	tr.mustLoad()
	return tr
}

// mustLoad parses all templates according to the pattern.
// It panics if parsing fails, enforcing fail-fast behavior.
// Called internally by NewTemplateRenderer and in dev mode.
func (tr *TemplateRenderer) mustLoad() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	parsed, err := template.New("").Funcs(tr.funcs).ParseGlob(tr.pattern)
	if err != nil {
		panic("failed to parse templates: " + err.Error())
	}
	tr.templates = parsed
}

// Render executes the template with the given name and data, writing
// the output to the provided http.ResponseWriter.
// In dev mode, templates are reloaded on each render.
func (tr *TemplateRenderer) Render(w http.ResponseWriter, name string, data interface{}) error {
	if tr.devMode {
		tr.mustLoad()
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()

	return tr.templates.ExecuteTemplate(w, name, data)
}

// Render is a helper on Context to render templates using a TemplateRenderer.
// It sets the response code, handles errors, and ensures the response
// is only written once per request.
//
// Parameters:
//   - renderer: the TemplateRenderer to use for rendering
//   - code: HTTP status code for the response
//   - name: template name to render
//   - data: data to pass into the template
//
// Returns a *Response indicating success or failure.
func (c *Context) Render(renderer *TemplateRenderer, code int, name string, data any) *Response {
	if c.Handled {
		return &Response{Success: false, Message: "Response already handled", Code: code}
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")

	// only write status here if it's not 200
	if code != http.StatusOK {
		c.Writer.WriteHeader(code)
	}

	err := renderer.Render(c.Writer, name, data)
	if err != nil {
		http.Error(c.Writer, "Template error: "+err.Error(), http.StatusInternalServerError)
		return &Response{Success: false, Message: "Template render error", Code: 500}
	}

	c.Handled = true
	return &Response{Success: true, Message: "Template rendered: " + name, Code: code}
}
