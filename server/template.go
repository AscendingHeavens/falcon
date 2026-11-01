package server

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
)

// TemplateRenderer renders HTML templates. Output is always escaped by html/template.
// Every file with a .html, .tmpl or .gohtml extension is a template, named by its
// slash-separated path relative to the root, such as "users/show.html".
//
// Files under the top-level layouts/ and partials/ folders are shared with every other
// template. Everything else is a page, parsed separately, so that any number of pages
// can define the same block without overwriting each other:
//
//	<!-- layouts/base.html -->
//	<html><body>{{template "partials/nav.html" .}}{{template "content" .}}</body></html>
//
//	<!-- users/show.html -->
//	{{template "layouts/base.html" .}}
//	{{define "content"}}<h1>{{.Name}}</h1>{{end}}
//
// It's safe for concurrent use.
type TemplateRenderer struct {
	fsys  fs.FS
	funcs template.FuncMap

	mu    sync.RWMutex
	pages map[string]*template.Template
}

// NewTemplateRenderer parses every template under dir.
// Example: NewTemplateRenderer("views", nil)
func NewTemplateRenderer(dir string, funcs template.FuncMap) (*TemplateRenderer, error) {
	return NewTemplateRendererFS(os.DirFS(dir), funcs)
}

// NewTemplateRendererFS parses every template in fsys. Use it with embed.FS to
// ship templates inside the binary, using fs.Sub if they're in a subfolder.
func NewTemplateRendererFS(fsys fs.FS, funcs template.FuncMap) (*TemplateRenderer, error) {
	tr := &TemplateRenderer{fsys: fsys, funcs: funcs}
	if err := tr.load(); err != nil {
		return nil, err
	}
	return tr, nil
}

// load parses all templates into a new set, which replaces the
// current one only if everything parses without errors.
func (tr *TemplateRenderer) load() (err error) {
	defer func() {
		// html/template panics on a FuncMap with a value that isn't a valid function.
		if r := recover(); r != nil {
			err = fmt.Errorf("falcon: invalid template functions: %v", r)
		}
	}()

	var shared, pages []string
	err = fs.WalkDir(tr.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch ext := path.Ext(p); {
		case d.IsDir() || (ext != ".html" && ext != ".tmpl" && ext != ".gohtml"):
		case strings.HasPrefix(p, "layouts/") || strings.HasPrefix(p, "partials/"):
			shared = append(shared, p)
		default:
			pages = append(pages, p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("falcon: reading templates: %w", err)
	}
	if len(shared)+len(pages) == 0 {
		return errors.New("falcon: no template files found")
	}

	base := template.New("").Funcs(tr.funcs)
	for _, p := range shared {
		if err := tr.parseInto(base, p); err != nil {
			return err
		}
	}

	set := make(map[string]*template.Template, len(shared)+len(pages))
	for _, p := range shared {
		if set[p], err = base.Clone(); err != nil {
			return fmt.Errorf("falcon: parsing template %s: %w", p, err)
		}
	}
	for _, p := range pages {
		page, err := base.Clone()
		if err != nil {
			return fmt.Errorf("falcon: parsing template %s: %w", p, err)
		}
		if err := tr.parseInto(page, p); err != nil {
			return err
		}
		set[p] = page
	}

	tr.mu.Lock()
	tr.pages = set
	tr.mu.Unlock()
	return nil
}

// parseInto parses one template file into t, under its own path as the name.
func (tr *TemplateRenderer) parseInto(t *template.Template, p string) error {
	content, err := fs.ReadFile(tr.fsys, p)
	if err != nil {
		return fmt.Errorf("falcon: reading template %s: %w", p, err)
	}
	if _, err := t.New(p).Parse(string(content)); err != nil {
		return fmt.Errorf("falcon: parsing template %s: %w", p, err)
	}
	return nil
}

// Render executes the named template and writes the output to w. Handlers should
// use c.Render instead; this is for rendering elsewhere, such as into an email.
func (tr *TemplateRenderer) Render(w io.Writer, name string, data any) error {
	tr.mu.RLock()
	page, ok := tr.pages[name]
	tr.mu.RUnlock()

	if !ok {
		return fmt.Errorf("falcon: template %q not found", name)
	}
	return page.ExecuteTemplate(w, name, data)
}

// renderBuffers recycles the buffers pages are rendered into.
var renderBuffers = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// releaseRenderBuffer returns a buffer to the pool, unless it has grown too large to keep.
func releaseRenderBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= maxPooledBuffer {
		buf.Reset()
		renderBuffers.Put(buf)
	}
}

// Render returns a response with the named template rendered using the server's renderer,
// set with app.SetRenderer. The page is rendered in full first, so a template that fails
// halfway gives a clean 500 and never a partial page. In debug mode, templates are parsed
// again on every call so that edits show up without a restart, and a template that no
// longer parses gives a 500 with the reason until it's fixed.
//
// Example: return c.Render(200, "users/show.html", user)
func (c *Context) Render(code int, name string, data any) *Response {
	settings := c.config()
	if settings.Renderer == nil {
		return c.Error(errors.New("falcon: c.Render needs a renderer, set one with app.SetRenderer"))
	}

	if settings.Debug {
		if err := settings.Renderer.load(); err != nil {
			return c.Error(err)
		}
	}

	buf := renderBuffers.Get().(*bytes.Buffer)
	if err := settings.Renderer.Render(buf, name, data); err != nil {
		releaseRenderBuffer(buf)
		return c.Error(fmt.Errorf("falcon: rendering template %q: %w", name, err))
	}
	return rawResponse(code, &rawBody{contentType: "text/html; charset=utf-8", data: buf.Bytes(), buffer: buf})
}
