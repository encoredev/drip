package email

import (
	"context"
	"crypto/subtle"
	"io"
	"net/http"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/mailgun/mailgun-go/v4"
	"github.com/russross/blackfriday/v2"
)

type SendParams struct {
	Template *Template
	Email    string
}

type SendResponse struct {
	ID   int64
	Sent bool
}

//encore:api auth
func Send(ctx context.Context, params *SendParams) (*SendResponse, error) {
	if err := ensureUserCreated(ctx, params.Email); err != nil {
		return nil, err
	}

	if status, err := isOptedIn(ctx, params.Email); err != nil {
		return nil, err
	} else if !status {
		return &SendResponse{Sent: false}, nil
	}

	tmpl := params.Template
	var id int64
	err := sqldb.QueryRow(ctx, `
		INSERT INTO "email" (email, template_id, created)
		VALUES ($1, $2, NOW())
		RETURNING id
	`, params.Email, tmpl.ID).Scan(&id)
	if err != nil {
		return nil, err
	}

	token, err := encodeEmailToken(params.Email, id)
	if err != nil {
		return nil, err
	}
	plaintext := strings.ReplaceAll(string(tmpl.TextBody), "{{Token}}", token)
	html := convertMarkdown(params.Template.HTMLBody, token)

	mg := mailgun.NewMailgun(cfg.MailgunDomain, secrets.MailGunAPIKey)
	msg := mg.NewMessage(params.Template.Sender, string(tmpl.Subject), plaintext, params.Email)
	msg.SetTrackingOpens(true)
	msg.SetTrackingClicks(false)
	msg.SetHtml(html)
	for _, f := range tmpl.Files {
		msg.AddReaderInline(f.Name, &imageReader{url: cfg.StrapiURL + f.URL})
	}

	// Use a different context for sending so we don't accidentally
	// send duplicate mails if the client disconnects.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, mailgunID, err := mg.Send(ctx, msg)
	if err != nil {
		return nil, err
	}

	_, err = sqldb.Exec(ctx, `
		UPDATE "email"
		SET mailgun_id = $2, sent = NOW()
		WHERE id = $1
	`, id, mailgunID)
	if err != nil {
		rlog.Error("failed to store mailgun id", "err", err)
	}

	return &SendResponse{ID: id, Sent: true}, nil
}

//encore:authhandler
func AuthHandler(ctx context.Context, token string) (auth.UID, error) {
	if subtle.ConstantTimeCompare([]byte(token), []byte(secrets.AuthPassword)) == 1 {
		return "ok", nil
	}
	return "", nil
}

type imageReader struct {
	url  string
	body io.ReadCloser
}

func (r *imageReader) Read(p []byte) (int, error) {
	if r.body == nil {
		resp, err := http.Get(r.url)
		if err != nil {
			return 0, err
		}
		r.body = resp.Body
	}
	return r.body.Read(p)
}

func (r *imageReader) Close() error {
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}

func convertMarkdown(template, token string) string {
	tmp := strings.ReplaceAll(template, "{{Token}}", token)
	rr := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{})
	r := htmlRenderer{HTMLRenderer: rr}
	html := blackfriday.Run([]byte(tmp), blackfriday.WithRenderer(r))
	return string(html)
}

type htmlRenderer struct {
	*blackfriday.HTMLRenderer
}

func (r htmlRenderer) RenderNode(w io.Writer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
	if node.Type == blackfriday.Image && entering {
		// Convert the image src to mailgun's cid notation.
		node.LinkData.Destination = []byte("cid:" + string(node.FirstChild.Literal))
	}
	return r.HTMLRenderer.RenderNode(w, node, entering)
}
