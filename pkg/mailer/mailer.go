package mailer

type Mailer interface {
	SendMail(from string, tos, ccs, bccs []string, replyTo, subject, body, htmltext string) error
}
