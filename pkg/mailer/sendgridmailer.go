package mailer

import (
	"html"
	"strings"

	"github.com/sendgrid/sendgrid-go"
)

type SendGridMailer struct {
	client *sendgrid.SGClient
}

func NewSendGridMailer(username, password string) *SendGridMailer {
	var cl *sendgrid.SGClient = nil
	if username != "" && password != "" {
		cl = sendgrid.NewSendGridClient(username, password)
	}
	return &SendGridMailer{client: cl}
}

func (s *SendGridMailer) SendMail(from string, tos, ccs, bccs []string, replyTo, subject, body, htmltext string) error {
	m := sendgrid.NewMail()
	m.SetFrom(from)

	if tos != nil {
		m.AddTos(tos)
	}
	if ccs != nil {
		m.AddCcs(ccs)
	}
	if bccs != nil {
		m.AddBccs(bccs)
	}

	m.SetReplyTo(replyTo)
	m.SetSubject(subject)
	m.SetText(body)
	if htmltext == "" {
		htmltext = html.EscapeString(body)
		htmltext = "<html><body>" + strings.Replace(htmltext, "\n", "<br>", -1) + "</body></html>"
	}
	m.SetHTML(htmltext)

	if s.client != nil {
		if err := s.client.Send(m); err != nil {
			return err
		}
	}
	return nil
}
