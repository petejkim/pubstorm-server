package fake

type Mailer struct {
	SendMailCalled bool
	From           string
	Tos            []string
	Ccs            []string
	Bccs           []string
	ReplyTo        string
	Subject        string
	Body           string
	HTML           string
	Error          error
}

func (s *Mailer) Reset() {
	s.SendMailCalled = false
	s.From = ""
	s.Tos = nil
	s.Ccs = nil
	s.Bccs = nil
	s.ReplyTo = ""
	s.Subject = ""
	s.Body = ""
	s.HTML = ""
	s.Error = nil
}

func (s *Mailer) SendMail(from string, tos, ccs, bccs []string, replyTo, subject, body, htmltext string) error {
	s.SendMailCalled = true
	s.From = from
	s.Tos = tos
	s.Ccs = ccs
	s.Bccs = bccs
	s.ReplyTo = replyTo
	s.Subject = subject
	s.Body = body
	s.HTML = htmltext
	return s.Error
}
