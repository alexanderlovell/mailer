package outbound

import (
	"text/template"
)

type rawSingleContext struct {
	From         string
	CombinedTo   string
	HasCC        bool
	CombinedCC   string
	HasReplyTo   bool
	ReplyTo      string
	MessageID    string
	HasInReplyTo bool
	InReplyTo    string
	Subject      string
	ContentType  string
	Body         string
	Date         string
}

var rawSingleTemplate = template.Must(template.New("rawsingle").Parse(
	`From: {{.From}}
To: {{.CombinedTo}}{{if .HasCC}}
Cc: {{.CombinedCC}}{{end}}{{ if .HasReplyTo }}
Reply-To: {{.ReplyTo}}{{end}}
MIME-Version: 1.0
Message-ID: <{{.MessageID}}>{{if .HasInReplyTo}}
In-Reply-To: {{.InReplyTo}}
References: {{.InReplyTo}}{{end}}
Content-Type: {{.ContentType}}
Content-Transfer-Encoding: quoted-printable
Subject: {{.Subject}}
Date: {{.Date}}

{{.Body}}
`))

type rawMultiContext struct {
	From         string
	CombinedTo   string
	HasCC        bool
	CombinedCC   string
	HasReplyTo   bool
	ReplyTo      string
	MessageID    string
	HasInReplyTo bool
	InReplyTo    string
	Boundary1    string
	Subject      string
	ContentType  string
	Body         string
	Files        []*emailFile
	Date         string
}

var rawMultiTemplate = template.Must(template.New("rawmulti").Parse(
	`From: {{.From}}
To: {{.CombinedTo}}{{if .HasCC}}
Cc: {{.CombinedCC}}{{end}}{{ if .HasReplyTo }}
Reply-To: {{.ReplyTo}}{{end}}
MIME-Version: 1.0
Message-ID: <{{.MessageID}}>{{if .HasInReplyTo}}
In-Reply-To: {{.InReplyTo}}
References: {{.InReplyTo}}{{end}}
Content-Type: multipart/mixed; boundary="{{.Boundary1}}"
Subject: {{.Subject}}
Date: {{.Date}}

--{{.Boundary1}}
Content-Type: {{.ContentType}}
Content-Transfer-Encoding: quoted-printable

{{.Body}}
--{{.Boundary1}}
{{ range .Files }}
--{{$.Boundary1}}
Content-Type: {{.Encoding}}
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="{{.Name}}"

{{.Body}}
{{ end }}
--{{.Boundary1}}--
`))

type pgpContext struct {
	From         string
	CombinedTo   string
	HasCC        bool
	CombinedCC   string
	HasReplyTo   bool
	ReplyTo      string
	MessageID    string
	HasInReplyTo bool
	InReplyTo    string
	ContentType  string
	Subject      string
	Body         string
	Date         string
}

var pgpTemplate = template.Must(template.New("rawmulti").Parse(
	`From: {{.From}}
To: {{.CombinedTo}}{{if .HasCC}}
Cc: {{.CombinedCC}}{{end}}{{ if .HasReplyTo }}
Reply-To: {{.ReplyTo}}{{end}}
MIME-Version: 1.0
Message-ID: <{{.MessageID}}>{{if .HasInReplyTo}}
In-Reply-To: {{.InReplyTo}}
References: {{.InReplyTo}}{{end}}
Content-Type: {{.ContentType}}
Subject: {{.Subject}}
Date: {{.Date}}

{{.Body}}
`))

type manifestSingleContext struct {
	From         string
	CombinedTo   string
	HasCC        bool
	CombinedCC   string
	HasReplyTo   bool
	ReplyTo      string
	MessageID    string
	HasInReplyTo bool
	InReplyTo    string
	Boundary1    string
	Subject      string
	SubjectHash  string
	Boundary2    string
	Body         string
	ID           string
	Manifest     string
	Date         string
}

var manifestSingleTemplate = template.Must(template.New("mansingle").Parse(
	`From: {{.From}}
To: {{.CombinedTo}}{{if .HasCC}}
Cc: {{.CombinedCC}}{{end}}{{ if .HasReplyTo }}
Reply-To: {{.ReplyTo}}{{end}}
MIME-Version: 1.0
Message-ID: <{{.MessageID}}>{{if .HasInReplyTo}}
In-Reply-To: {{.InReplyTo}}
References: {{.InReplyTo}}{{end}}
Content-Type: multipart/mixed; boundary="{{.Boundary1}}"
Subject: {{.Subject}}
Subject-Hash: {{.SubjectHash}}
Date: {{.Date}}

--{{.Boundary1}}
Content-Type: multipart/alternative; boundary="{{.Boundary2}}"

--{{.Boundary2}}
Content-Type: application/pgp-encrypted

{{.Body}}
--{{.Boundary2}}
Content-Type: text/html; charset="UTF-8"

<!DOCTYPE html>
<html>
<body>
<p>This is an encrypted email, <a href="https://view.lavaboom.com/#{{.ID}}">
open it here if you email client doesn't support PGP manifests
</a></p>
</body>
</html>
--{{.Boundary2}}
Content-Type: text/plain; charset="UTF-8"

This is an encrypted email, open it here if your email client
doesn't support PGP manifests:
https://view.lavaboom.com/#{{.ID}}
--{{.Boundary2}}--
--{{.Boundary1}}
Content-Type: application/x-pgp-manifest+json
Content-Disposition: attachment; filename="manifest.pgp"

{{.Manifest}}
--{{.Boundary1}}--
`))

type manifestMultiContext struct {
	From         string
	CombinedTo   string
	HasCC        bool
	CombinedCC   string
	HasReplyTo   bool
	ReplyTo      string
	MessageID    string
	HasInReplyTo bool
	InReplyTo    string
	Boundary1    string
	Subject      string
	SubjectHash  string
	Boundary2    string
	Body         string
	ID           string
	Files        []*emailFile
	Manifest     string
	Date         string
}

var manifestMultiTemplate = template.Must(template.New("manmulti").Parse(
	`From: {{.From}}
To: {{.CombinedTo}}{{if .HasCC}}
Cc: {{.CombinedCC}}{{end}}{{ if .HasReplyTo }}
Reply-To: {{.ReplyTo}}{{end}}
MIME-Version: 1.0
Message-ID: <{{.MessageID}}>{{if .HasInReplyTo}}
In-Reply-To: {{.InReplyTo}}
References: {{.InReplyTo}}{{end}}
Content-Type: multipart/mixed; boundary="{{.Boundary1}}"
Subject: {{.Subject}}
Subject-Hash: {{.SubjectHash}}
Date: {{.Date}}

--{{.Boundary1}}
Content-Type: multipart/alternative; boundary="{{.Boundary2}}"

--{{.Boundary2}}
Content-Type: application/pgp-encrypted

{{.Body}}
--{{.Boundary2}}
Content-Type: text/html; charset="UTF-8"

<!DOCTYPE html>
<html>
<body>
<p>This is an encrypted email, <a href="https://view.lavaboom.com/#{{.ID}}">
open it here if you email client doesn't support PGP manifests
</a></p>
</body>
</html>
--{{.Boundary2}}
Content-Type: text/plain; charset="UTF-8"

This is an encrypted email, open it here if your email client
doesn't support PGP manifests:
https://view.lavaboom.com/#{{.ID}}
--{{.Boundary2}}--{{ range .Files }}
--{{$.Boundary1}}
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="{{.Name}}"

{{.Body}}
{{ end }}
--{{.Boundary1}}
Content-Type: application/x-pgp-manifest+json
Content-Disposition: attachment; filename="manifest.pgp"

{{.Manifest}}
--{{.Boundary1}}--
`))

type emailFile struct {
	Name     string
	Body     string
	Encoding string
}
