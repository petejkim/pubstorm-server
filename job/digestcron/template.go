package main

import (
	//"github.com/nitrous-io/rise-server/apiserver/stat"
	"bytes"
	"html/template"
)

var bodyHtmlTemplate = `
   Those are the stats for your project ({{ .ProjectName }}):
{{range $stat := .Stats }}
	Stats for {{ $stat.DomainName}} ({{ $stat.From }} - {{ $stat.To }}):
	<ul>
	<li>Total bandwidth in bytes: {{ $stat.TotalBandwidth }}
	<li>Total requests: {{ $stat.TotalRequests }}
	<li>Unique visitors: {{ $stat.UniqueVisitors }}
	<li>Total Page Views: {{ $stat.TotalPageViews }}
	<li>Top Pages
		<ul>
		{{ range $pageview := $stat.TopPageViews -}}
		<li> {{ $pageview.Path }} - {{ $pageview.Count }} </li>
		{{ end}}
		</ul>
	</ul>
{{ end }}
`

var bodyTextTemplate = `
Those are the stats for your project ({{ .ProjectName }}):
{{range $stat := .Stats }}
	Stats for {{ $stat.DomainName}} ({{ $stat.From }} - {{ $stat.To }}):
	- Total bandwidth in bytes: {{ $stat.TotalBandwidth }}
	- Total requests: {{ $stat.TotalRequests }}
	- Unique visitors: {{ $stat.UniqueVisitors }}
	- Total Page Views: {{ $stat.TotalPageViews }}
	- Top Pages
		{{ range $pageview := $stat.TopPageViews -}}
		- {{ $pageview.Path }} - {{ $pageview.Count }}
		{{ end}}
{{ end }}

`

func generateEmailBody(stats *Stats) (body string, bodyHtml string, err error) {
	var bodyBuffer, bodyHtmlBuffer bytes.Buffer
	tBody, err := template.New("body").Parse(bodyTextTemplate)
	if err != nil {
		return "", "", err
	}

	err = tBody.ExecuteTemplate(&bodyBuffer, "body", stats)
	if err != nil {
		return "", "", err
	}

	tHtmlBody, err := template.New("body").Parse(bodyHtmlTemplate)
	if err != nil {
		return "", "", err
	}

	err = tHtmlBody.ExecuteTemplate(&bodyHtmlBuffer, "body", stats)
	if err != nil {
		return "", "", err
	}

	return bodyBuffer.String(), bodyHtmlBuffer.String(), nil
}
