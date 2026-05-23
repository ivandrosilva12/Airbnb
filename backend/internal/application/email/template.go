package emailapp

import (
	"bytes"
	"html/template"
	"log/slog"
)

// layout is the shared HTML shell for transactional emails. Dynamic fields are
// auto-escaped by html/template, so untrusted values (e.g. property titles)
// cannot inject markup.
var layout = template.Must(template.New("email").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Heading}}</title>
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f4f5;padding:24px 0;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:12px;overflow:hidden;border:1px solid #ececec;">
<tr><td style="background:#ff385c;padding:20px 28px;">
<span style="color:#ffffff;font-size:20px;font-weight:700;letter-spacing:-0.3px;">AirHost</span>
</td></tr>
<tr><td style="padding:28px;">
<h1 style="margin:0 0 12px;font-size:18px;line-height:1.3;color:#111111;">{{.Heading}}</h1>
<p style="margin:0;font-size:15px;line-height:1.55;color:#444444;">{{.Body}}</p>
</td></tr>
<tr><td style="padding:18px 28px;border-top:1px solid #eeeeee;">
<p style="margin:0;font-size:12px;line-height:1.5;color:#999999;">You're receiving this because you have an AirHost account.</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`))

// renderHTML wraps a heading and plaintext body in the branded HTML layout.
func renderHTML(heading, body string) string {
	var b bytes.Buffer
	if err := layout.Execute(&b, struct{ Heading, Body string }{heading, body}); err != nil {
		slog.Error("email: render html failed", "error", err)
		return ""
	}
	return b.String()
}
