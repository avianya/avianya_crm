// Package mail sends plain-text email over SMTP, using only the standard
// library. It is deliberately small: one message to one recipient, which is
// all reminders need.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"crm-lead-management/backend/internal/config"
)

// timeout bounds a whole send: connect, TLS, auth and the message itself. A
// mail server that hangs must not stall the reminder job.
const timeout = 30 * time.Second

// ErrNotConfigured is returned by Send when no SMTP server is set up.
var ErrNotConfigured = errors.New("email is not set up on the server (SMTP settings are missing)")

// Sender sends email through one SMTP server.
type Sender struct {
	cfg config.SMTPConfig
}

// New returns a Sender for cfg. It does not connect; that happens per send.
func New(cfg config.SMTPConfig) *Sender {
	// Google shows app passwords in groups of four; the spaces are not part of it.
	cfg.Password = strings.ReplaceAll(cfg.Password, " ", "")
	return &Sender{cfg: cfg}
}

// Configured reports whether email can be attempted at all.
func (s *Sender) Configured() bool { return s.cfg.Configured() }

// From is the sender address as configured, for display. Never the password.
func (s *Sender) From() string { return s.cfg.From }

// Message is one plain-text email to one recipient.
type Message struct {
	To      string
	Subject string
	Body    string

	// ReplyTo, when set, is where replies go instead of the From address --
	// the lead's owner, for an email written to a lead.
	ReplyTo string

	// Automated marks the message as machine-generated (reminders, summaries),
	// so mail servers do not send out-of-office replies to it. An email a
	// person wrote to a lead is not automated.
	Automated bool
}

// Send delivers one automated plain-text message, as reminders and summaries
// are. See SendMessage.
func (s *Sender) Send(ctx context.Context, to, subject, body string) error {
	return s.SendMessage(ctx, Message{To: to, Subject: subject, Body: body, Automated: true})
}

// SendMessage delivers one plain-text message. It returns only when the server
// has accepted the message for delivery, or with the reason it did not -- the
// server's own reply where there is one, never the password.
func (s *Sender) SendMessage(ctx context.Context, m Message) error {
	if !s.Configured() {
		return ErrNotConfigured
	}

	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return fmt.Errorf("SMTP_FROM %q is not a valid address", s.cfg.From)
	}
	rcpt, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("recipient %q is not a valid address", m.To)
	}
	var replyTo *mail.Address
	if m.ReplyTo != "" {
		if replyTo, err = mail.ParseAddress(m.ReplyTo); err != nil {
			return fmt.Errorf("reply-to %q is not a valid address", m.ReplyTo)
		}
	}
	// Header injection: a newline in any header value would let it add headers
	// or recipients of its own.
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("subject contains a line break")
	}

	message, err := buildMessage(from, rcpt, replyTo, m.Subject, m.Body, m.Automated)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return s.deliver(ctx, from.Address, rcpt.Address, message)
}

func (s *Sender) deliver(ctx context.Context, from, to string, message []byte) error {
	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	implicitTLS := s.cfg.Port == "465"
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("could not connect to %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP greeting from %s failed: %w", addr, err)
	}
	defer client.Close()

	encrypted := implicitTLS
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS with %s failed: %w", addr, err)
			}
			encrypted = true
		}
	}

	if s.cfg.Username != "" {
		// Never send a password in the clear. smtp.PlainAuth refuses too, but
		// saying why here gives a far clearer message.
		if !encrypted && !isLocalhost(s.cfg.Host) {
			return fmt.Errorf("%s does not offer encryption, so the password was not sent", addr)
		}
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("login to %s was refused: %w", s.cfg.Host, err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("server refused the sender %s: %w", from, err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("server refused the recipient %s: %w", to, err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("server refused the message: %w", err)
	}
	if _, err := w.Write(message); err != nil {
		return fmt.Errorf("sending the message failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("server did not accept the message: %w", err)
	}
	return client.Quit()
}

// buildMessage renders the headers and a UTF-8 quoted-printable body. Quoted-
// printable keeps lines short and 7-bit clean, so names like "Aarav Shāh"
// survive any mail server.
//
// replyTo may be nil.
func buildMessage(from, to, replyTo *mail.Address, subject, body string, automated bool) ([]byte, error) {
	var b bytes.Buffer
	host := "localhost"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 {
		host = from.Address[at+1:]
	}

	headers := []string{
		"From: " + from.String(),
		"To: " + to.String(),
	}
	if replyTo != nil {
		headers = append(headers, "Reply-To: "+replyTo.String())
	}
	headers = append(headers,
		"Subject: "+mime.QEncoding.Encode("utf-8", subject),
		"Date: "+time.Now().Format(time.RFC1123Z),
		fmt.Sprintf("Message-ID: <%d.crm@%s>", time.Now().UnixNano(), host),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
	)
	if automated {
		headers = append(headers, "Auto-Submitted: auto-generated")
	}
	for _, h := range headers {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	// SMTP wants CRLF line endings in the body too.
	normalized := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := qp.Write([]byte(normalized)); err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	return b.Bytes(), nil
}

func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
