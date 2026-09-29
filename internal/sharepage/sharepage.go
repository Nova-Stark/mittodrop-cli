package sharepage

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed page.html
var pageHTML string

//go:embed styles.css
var pageCSS string

//go:embed app.js
var pageJS string

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = template.New("sharepage").Parse(pageHTML)
	if err != nil {
		panic(fmt.Sprintf("sharepage: parse template: %v", err))
	}
}

type templateData struct {
	DeviceName string
	Styles     template.CSS
	Script     template.JS
}

// Render writes rendered responsive dropzone webpage to w.
func Render(w io.Writer, deviceName string) error {
	data := templateData{
		DeviceName: deviceName,
		Styles:     template.CSS(pageCSS),
		Script:     template.JS(pageJS),
	}
	return tmpl.Execute(w, data)
}
