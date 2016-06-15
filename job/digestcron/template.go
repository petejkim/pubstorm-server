package main

import (
	//"github.com/nitrous-io/rise-server/apiserver/stat"
	"bytes"
	"fmt"
	"html/template"
)

var bodyHtmlTemplate = `
   Those are the stats for your project ({{ .ProjectName }}):<br/>
{{range $stat := .Stats }}
	Stats for {{ $stat.DomainName}} ({{ $stat.From.Format "2 January 2006" }} - {{ $stat.To.Format "2 January 2006" }}):
	<ul>
	<li>Total bandwidth in bytes: {{ megabytes $stat.TotalBandwidth }}
	<li>Total requests: {{ $stat.TotalRequests }}
	<li>Unique visitors: {{ $stat.UniqueVisitors }}
	<li>Total Page Views: {{ $stat.TotalPageViews }}
	<li>Top Pages
		<ul>
		{{ range $pageview := $stat.TopPageViews -}}
		<li>{{ $pageview.Count | printf "%5d" }} - {{ $pageview.Path }}</li>
		{{ end}}
		</ul>
	</ul>
{{ end }}
`

var bodyTextTemplate = `
Those are the stats for your project ({{ .ProjectName }}):
{{range $stat := .Stats }}
	Stats for {{ $stat.DomainName}} ({{ $stat.From.Format "2 January 2000" }} - {{ $stat.To.Format "2 January 2000" }}):
	- Total bandwidth in bytes: {{ megabytes $stat.TotalBandwidth }}
	- Total requests: {{ $stat.TotalRequests }}
	- Unique visitors: {{ $stat.UniqueVisitors }}
	- Total Page Views: {{ $stat.TotalPageViews }}
	- Top Pages
		{{ range $pageview := $stat.TopPageViews -}}
		- {{ $pageview.Count | printf "%5d" }} - {{ $pageview.Path }}
		{{ end}}
{{ end }}

`

func generateEmailBody(stats *Stats) (body string, bodyHtml string, err error) {
	fm := template.FuncMap{"megabytes": func(a float64) string {
		return fmt.Sprintf("%.2f Mb", a/1024/1024)
	}}
	var bodyBuffer, bodyHtmlBuffer bytes.Buffer
	tBody, err := template.New("body").Funcs(fm).Parse(bodyTextTemplate)
	if err != nil {
		return "", "", err
	}

	err = tBody.ExecuteTemplate(&bodyBuffer, "body", stats)
	if err != nil {
		return "", "", err
	}

	tHtmlBody, err := template.New("body").Funcs(fm).Parse(bodyHtmlTemplate)
	if err != nil {
		return "", "", err
	}

	err = tHtmlBody.ExecuteTemplate(&bodyHtmlBuffer, "body", stats)
	if err != nil {
		return "", "", err
	}

	return bodyBuffer.String(), bodyHtmlBuffer.String(), nil
}
