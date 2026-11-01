package server

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func file(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

// views is a typical template folder: a layout, a partial, and pages sharing a block name.
func views() fstest.MapFS {
	return fstest.MapFS{
		"layouts/base.html":  file(`<html>{{template "partials/nav.html" .}}<main>{{template "content" .}}</main></html>`),
		"partials/nav.html":  file(`<nav>{{.Site}}</nav>`),
		"home.html":          file(`{{template "layouts/base.html" .}}{{define "content"}}<h1>Home {{.Name}}</h1>{{end}}`),
		"users/show.html":    file(`{{template "layouts/base.html" .}}{{define "content"}}<h1>User {{.Name}}</h1>{{end}}`),
		"admin/show.html":    file(`{{template "layouts/base.html" .}}{{define "content"}}<h1>Admin {{.Name}}</h1>{{end}}`),
		"plain.tmpl":         file(`plain {{.Name}}`),
		"plain2.gohtml":      file(`gohtml {{.Name}}`),
		"notes.txt":          file(`{{this isn't a template and must not be parsed`),
		"assets/logo.svg":    file(`{{nor is this`),
		"users/noblock.html": file(`{{template "layouts/base.html" .}}`),
	}
}

type pageData struct {
	Site string
	Name string
}

var testData = pageData{Site: "Falcon", Name: "Ada"}

func newRendering(t *testing.T, fsys fstest.MapFS, funcs template.FuncMap, debug bool) *Context {
	t.Helper()
	tr, err := NewTemplateRendererFS(fsys, funcs)
	assert.NoError(t, err)

	c, _ := newResponseContext(http.MethodGet)
	c.settings = &Settings{Renderer: tr, Debug: debug}
	return c
}

func TestRender_DescribesThenSends(t *testing.T) {
	c := newRendering(t, views(), nil, false)
	rec := c.Writer.(*httptest.ResponseRecorder)

	resp := c.Render(201, "plain.tmpl", testData)

	assert.True(t, resp.IsRaw())
	assert.True(t, resp.Success)
	assert.Equal(t, 201, resp.Code)
	assert.Equal(t, "text/html; charset=utf-8", resp.ContentType())
	assert.False(t, c.Handled, "nothing is written until the server sends it")

	c.WriteResponse(resp)
	res := rec.Result()
	assert.Equal(t, 201, res.StatusCode)
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))
}

func rendered(t *testing.T, c *Context, name string, data any) (int, string) {
	t.Helper()
	rec := c.Writer.(*httptest.ResponseRecorder)
	c.WriteResponse(c.Render(200, name, data))
	return rec.Code, rec.Body.String()
}

func TestRender_NamesAndExtensions(t *testing.T) {
	for name, want := range map[string]string{
		"plain.tmpl":    "plain Ada",
		"plain2.gohtml": "gohtml Ada",
	} {
		code, body := rendered(t, newRendering(t, views(), nil, false), name, testData)
		assert.Equal(t, 200, code, name)
		assert.Equal(t, want, body)
	}
}

func TestRender_OnlyTemplateFilesAreParsed(t *testing.T) {
	// notes.txt and logo.svg would fail to parse, so getting here at all proves they were skipped.
	c := newRendering(t, views(), nil, false)

	code, _ := rendered(t, c, "notes.txt", testData)
	assert.Equal(t, 500, code)
}

// This is the failure that stops layouts working with a single template set:
// every page defining "content" would overwrite the others.
func TestRender_PagesWithTheSameBlockDontCollide(t *testing.T) {
	for name, want := range map[string]string{
		"home.html":       "<h1>Home Ada</h1>",
		"users/show.html": "<h1>User Ada</h1>",
		"admin/show.html": "<h1>Admin Ada</h1>",
	} {
		code, body := rendered(t, newRendering(t, views(), nil, false), name, testData)
		assert.Equal(t, 200, code, name)
		assert.Equal(t, "<html><nav>Falcon</nav><main>"+want+"</main></html>", body)
	}
}

func TestRender_SharedTemplatesCanBeRenderedDirectly(t *testing.T) {
	_, body := rendered(t, newRendering(t, views(), nil, false), "partials/nav.html", testData)
	assert.Equal(t, "<nav>Falcon</nav>", body)
}

func TestRender_EscapesOutput(t *testing.T) {
	for _, name := range []string{"home.html", "plain.tmpl", "plain2.gohtml"} {
		_, body := rendered(t, newRendering(t, views(), nil, false), name, pageData{Name: "<script>alert(1)</script>"})
		assert.NotContains(t, body, "<script>", name)
		assert.Contains(t, body, "&lt;script&gt;", name)
	}
}

func TestRender_Funcs(t *testing.T) {
	fsys := fstest.MapFS{"a.html": file(`{{shout .Name}}`)}
	funcs := template.FuncMap{"shout": strings.ToUpper}

	_, body := rendered(t, newRendering(t, fsys, funcs, false), "a.html", testData)

	assert.Equal(t, "ADA", body)
}

func TestRender_FailuresAreCleanEnvelopes(t *testing.T) {
	failing := fstest.MapFS{
		"halfway.html": file(`<h1>partial output</h1>{{fail}}<p>never</p>`),
		"nofield.html": file(`<h1>partial output</h1>{{.Nope}}`),
	}
	funcs := template.FuncMap{"fail": func() (string, error) { return "", errors.New("db password is hunter2") }}

	tests := []struct{ name, template string }{
		{"a template that fails halfway", "halfway.html"},
		{"a field that doesn't exist", "nofield.html"},
		{"an unknown template", "nope.html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRendering(t, failing, funcs, false)
			rec := c.Writer.(*httptest.ResponseRecorder)

			resp := c.Render(200, tt.template, testData)
			assert.False(t, resp.IsRaw())
			c.WriteResponse(resp)

			assert.Equal(t, 500, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"success":false,"message":"Internal Server Error","code":500}`, rec.Body.String(),
				"no partial page, and no internals")
		})
	}

	t.Run("debug mode shows the reason", func(t *testing.T) {
		c := newRendering(t, failing, funcs, true)
		resp := c.Render(200, "halfway.html", testData)
		assert.Contains(t, resp.Details, "hunter2")
	})

	t.Run("a layout whose block a page didn't define", func(t *testing.T) {
		code, _ := rendered(t, newRendering(t, views(), nil, false), "users/noblock.html", testData)
		assert.Equal(t, 500, code)
	})
}

func TestRender_WithoutARendererSaysHowToFixIt(t *testing.T) {
	c, _ := newResponseContext(http.MethodGet)
	c.settings = &Settings{Debug: true}

	resp := c.Render(200, "a.html", nil)

	assert.Equal(t, 500, resp.Code)
	assert.Contains(t, resp.Details, "app.SetRenderer")
}

func TestRender_DebugReloadsOnEveryRender(t *testing.T) {
	fsys := fstest.MapFS{"a.html": file(`before`)}
	debug := newRendering(t, fsys, nil, true)

	_, body := rendered(t, debug, "a.html", nil)
	assert.Equal(t, "before", body)

	fsys["a.html"] = file(`after`)
	fsys["new.html"] = file(`brand new`)

	c, _ := newResponseContext(http.MethodGet)
	c.settings = debug.settings
	_, body = rendered(t, c, "a.html", nil)
	assert.Equal(t, "after", body)

	c, _ = newResponseContext(http.MethodGet)
	c.settings = debug.settings
	_, body = rendered(t, c, "new.html", nil)
	assert.Equal(t, "brand new", body)
}

func TestRender_WithoutDebugDoesNotReload(t *testing.T) {
	fsys := fstest.MapFS{"a.html": file(`before`)}
	first := newRendering(t, fsys, nil, false)
	_, body := rendered(t, first, "a.html", nil)
	assert.Equal(t, "before", body)

	fsys["a.html"] = file(`after`)

	c, _ := newResponseContext(http.MethodGet)
	c.settings = first.settings
	_, body = rendered(t, c, "a.html", nil)
	assert.Equal(t, "before", body)
}

func TestRender_DebugShowsABrokenEditThenRecovers(t *testing.T) {
	fsys := fstest.MapFS{"a.html": file(`good`), "b.html": file(`also good`)}
	settings := newRendering(t, fsys, nil, true).settings
	render := func(name string) (int, string) {
		c, _ := newResponseContext(http.MethodGet)
		c.settings = settings
		return rendered(t, c, name, nil)
	}

	fsys["a.html"] = file(`{{broken`)

	code, body := render("a.html")
	assert.Equal(t, 500, code, "a broken edit must be visible, not hidden behind stale output")
	assert.Contains(t, body, "a.html", "the reason names the file")

	var direct strings.Builder
	assert.NoError(t, settings.Renderer.Render(&direct, "a.html", nil))
	assert.Equal(t, "good", direct.String(), "the last good set is kept")

	fsys["a.html"] = file(`fixed`)

	code, body = render("a.html")
	assert.Equal(t, 200, code)
	assert.Equal(t, "fixed", body)
}

func TestNewTemplateRenderer_Errors(t *testing.T) {
	tests := []struct {
		name  string
		fsys  fstest.MapFS
		funcs template.FuncMap
		want  string
	}{
		{"no templates", fstest.MapFS{"notes.txt": file("x")}, nil, "no template files"},
		{"a page with a syntax error", fstest.MapFS{"bad.html": file("{{oops")}, nil, "bad.html"},
		{"a shared file with a syntax error", fstest.MapFS{"layouts/bad.html": file("{{oops")}, nil, "layouts/bad.html"},
		{"an unknown function", fstest.MapFS{"a.html": file("{{nope}}")}, nil, "a.html"},
		{"an invalid function", fstest.MapFS{"a.html": file("x")}, template.FuncMap{"bad": "not a function"}, "invalid template functions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTemplateRendererFS(tt.fsys, tt.funcs)
			assert.Nil(t, tr)
			assert.ErrorContains(t, err, tt.want)
		})
	}

	t.Run("a folder that doesn't exist", func(t *testing.T) {
		tr, err := NewTemplateRenderer(filepath.Join(t.TempDir(), "missing"), nil)
		assert.Nil(t, tr)
		assert.Error(t, err)
	})
}

func TestNewTemplateRenderer_FromADirectory(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "users"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "users", "show.html"), []byte(`hi {{.Name}}`), 0o644))

	tr, err := NewTemplateRenderer(dir, nil)
	assert.NoError(t, err)

	var out strings.Builder
	assert.NoError(t, tr.Render(&out, "users/show.html", testData), "the name uses forward slashes on every OS")
	assert.Equal(t, "hi Ada", out.String())
}

func TestRender_PooledBuffersDontLeakBetweenPages(t *testing.T) {
	fsys := fstest.MapFS{
		"big.html":   file(`{{.Name}}`),
		"small.html": file(`small`),
	}
	settings := newRendering(t, fsys, nil, false).settings
	render := func(name string, data any) string {
		c, _ := newResponseContext(http.MethodGet)
		c.settings = settings
		_, body := rendered(t, c, name, data)
		return body
	}

	// One over the limit of what's kept, and several within it.
	assert.Len(t, render("big.html", pageData{Name: strings.Repeat("x", maxPooledBuffer*2)}), maxPooledBuffer*2)
	for i := range 20 {
		assert.Len(t, render("big.html", pageData{Name: strings.Repeat("y", 1000)}), 1000, i)
		assert.Equal(t, "small", render("small.html", nil))
	}
}

// Run with -race: reloads swap the template set while other requests are rendering from it.
func TestRender_ConcurrentWithDebugReloads(t *testing.T) {
	settings := newRendering(t, views(), nil, true).settings

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 25 {
				c, _ := newResponseContext(http.MethodGet)
				c.settings = settings
				name := fmt.Sprintf("%d-%d", i, j)
				_, body := rendered(t, c, "users/show.html", pageData{Site: "F", Name: name})
				assert.Contains(t, body, "<h1>User "+name+"</h1>")
			}
		}()
	}
	wg.Wait()
}
