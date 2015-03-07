package outbound

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/alexcesaro/quotedprintable"
	"github.com/bitly/go-nsq"
	"github.com/blang/semver"
	"github.com/dancannon/gorethink"
	"github.com/dchest/uniuri"
	"github.com/eaigner/dkim"
	"github.com/lavab/api/models"
	"github.com/lavab/mailer/shared"
	man "github.com/lavab/pgp-manifest-go"
	"golang.org/x/crypto/openpgp"
)

var domains = map[string]struct{}{
	"lavaboom.com": struct{}{},
	"lavaboom.io":  struct{}{},
	"lavaboom.co":  struct{}{},
}

func StartQueue(config *shared.Flags) {
	// Initialize a new logger
	log := logrus.New()
	if config.LogFormatterType == "text" {
		log.Formatter = &logrus.TextFormatter{
			ForceColors: config.ForceColors,
		}
	} else if config.LogFormatterType == "json" {
		log.Formatter = &logrus.JSONFormatter{}
	}

	log.Level = logrus.DebugLevel

	// Initialize the database connection
	session, err := gorethink.Connect(gorethink.ConnectOpts{
		Address: config.RethinkAddress,
		AuthKey: config.RethinkKey,
		MaxIdle: 10,
		Timeout: time.Second * 10,
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatal("Unable to connect to RethinkDB")
	}

	// Create a new producer
	consumer, err := nsq.NewConsumer("send_email", "receive", nsq.NewConfig())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatal("Unable to create a consumer")
	}

	// Connect to NSQ
	producer, err := nsq.NewProducer(config.NSQDAddress, nsq.NewConfig())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatal("Unable to connect to NSQd")
	}

	// Load a DKIM signer
	var dkimSigner map[string]*dkim.DKIM
	if config.DKIMKey != "" {
		dkimSigner = map[string]*dkim.DKIM{}

		key, err := ioutil.ReadFile(config.DKIMKey)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Fatal("Unable to read DKIM private key")
		}

		for domain, _ := range domains {
			dkimConf, err := dkim.NewConf(domain, config.DKIMSelector)
			if err != nil {
				log.WithFields(logrus.Fields{
					"error": err.Error(),
				}).Fatal("Unable to create a new DKIM conf object")
			}

			dk, err := dkim.New(dkimConf, key)
			if err != nil {
				log.WithFields(logrus.Fields{
					"error": err.Error(),
				}).Fatal("Unable to create a new DKIM signer")
			}

			dkimSigner[domain] = dk
		}
	}

	consumer.AddConcurrentHandlers(nsq.HandlerFunc(func(msg *nsq.Message) error {
		var id string
		if err := json.Unmarshal(msg.Body, &id); err != nil {
			return err
		}

		idHash := sha256.Sum256([]byte(id))
		messageID := hex.EncodeToString(idHash[:]) + "@" + config.Hostname

		// Get the email from the database
		cursor, err := gorethink.Db(config.RethinkDatabase).Table("emails").Get(id).Run(session)
		if err != nil {
			return err
		}
		var email *models.Email
		if err := cursor.One(&email); err != nil {
			return err
		}

		// Get the thread
		cursor, err = gorethink.Db(config.RethinkDatabase).Table("threads").Get(email.Thread).Run(session)
		if err != nil {
			return err
		}
		var thread *models.Thread
		if err := cursor.One(&thread); err != nil {
			return err
		}

		// Fetch the files
		var files []*models.File
		if email.Files != nil && len(email.Files) > 0 {
			filesList := []interface{}{}
			for _, v := range email.Files {
				filesList = append(filesList, v)
			}
			cursor, err = gorethink.Db(config.RethinkDatabase).Table("files").GetAll(filesList...).Run(session)
			if err != nil {
				return err
			}
			if err := cursor.All(&files); err != nil {
				return err
			}
		} else {
			files = []*models.File{}
		}

		// Fetch the owner
		cursor, err = gorethink.Db(config.RethinkDatabase).Table("accounts").Get(email.Owner).Run(session)
		if err != nil {
			return err
		}
		var account *models.Account
		if err := cursor.One(&account); err != nil {
			return err
		}

		// Declare a contents variable
		contents := ""

		ctxFrom := email.From
		if v1, ok := account.Settings.(map[string]interface{}); ok {
			if v2, ok := v1["displayName"]; ok {
				if v3, ok := v2.(string); ok {
					ctxFrom = v3 + " <" + ctxFrom + ">"
				}
			}
		}

		if email.Kind == "raw" {
			// Encode the email
			if files == nil || len(files) == 0 {
				buffer := &bytes.Buffer{}

				context := &rawSingleContext{
					From:        ctxFrom,
					CombinedTo:  strings.Join(email.To, ", "),
					MessageID:   messageID,
					Subject:     quotedprintable.EncodeToString([]byte(email.Name)),
					ContentType: email.ContentType,
					Body:        quotedprintable.EncodeToString([]byte(email.Body)),
				}

				if email.CC != nil && len(email.CC) > 0 {
					context.HasCC = true
					context.CombinedCC = strings.Join(email.CC, ", ")
				}

				if email.ReplyTo != "" {
					context.HasReplyTo = true
					context.ReplyTo = email.ReplyTo
				}

				if err := rawSingleTemplate.Execute(buffer, context); err != nil {
					return err
				}

				contents = buffer.String()
			} else {
				buffer := &bytes.Buffer{}

				emailFiles := []*emailFile{}
				for _, file := range files {
					emailFiles = append(emailFiles, &emailFile{
						Encoding: file.Encoding,
						Name:     file.Name,
						Body:     base64.StdEncoding.EncodeToString([]byte(file.Data)),
					})
				}

				context := &rawMultiContext{
					From:        ctxFrom,
					CombinedTo:  strings.Join(email.To, ", "),
					MessageID:   messageID,
					Boundary1:   uniuri.NewLen(20),
					Subject:     quotedprintable.EncodeToString([]byte(email.Name)),
					ContentType: email.ContentType,
					Body:        quotedprintable.EncodeToString([]byte(email.Body)),
					Files:       emailFiles,
				}

				if email.CC != nil && len(email.CC) > 0 {
					context.HasCC = true
					context.CombinedCC = strings.Join(email.CC, ", ")
				}

				if email.ReplyTo != "" {
					context.HasReplyTo = true
					context.ReplyTo = email.ReplyTo
				}

				if err := rawMultiTemplate.Execute(buffer, context); err != nil {
					return err
				}

				contents = buffer.String()
			}

			// Fetch owner's account
			cursor, err = gorethink.Db(config.RethinkDatabase).Table("accounts").Get(email.Owner).Run(session)
			if err != nil {
				return err
			}
			var account *models.Account
			if err := cursor.One(&account); err != nil {
				return err
			}

			// Get owner's key
			var key *models.Key
			if account.PublicKey != "" {
				cursor, err = gorethink.Db(config.RethinkDatabase).Table("keys").Get(account.PublicKey).Run(session)
				if err != nil {
					return err
				}
				if err := cursor.One(&key); err != nil {
					return err
				}
			} else {
				cursor, err = gorethink.Db(config.RethinkDatabase).Table("keys").GetAllByIndex("owner", account.ID).Run(session)
				if err != nil {
					return err
				}
				var keys []*models.Key
				if err := cursor.All(&keys); err != nil {
					return err
				}

				key = keys[0]
			}

			// Parse the key
			keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key.Key))
			if err != nil {
				return err
			}

			// From, to and cc parsing
			fromAddr, err := mail.ParseAddress(email.From)
			if err != nil {
				fromAddr = &mail.Address{
					Address: email.From,
				}
			}
			toAddr, err := mail.ParseAddressList(strings.Join(email.To, ", "))
			if err != nil {
				toAddr = []*mail.Address{}
				for _, addr := range email.To {
					toAddr = append(toAddr, &mail.Address{
						Address: addr,
					})
				}
			}

			// Prepare a new manifest
			manifest := &man.Manifest{
				Version: semver.Version{1, 0, 0, nil, nil},
				From:    fromAddr,
				To:      toAddr,
				Subject: email.Name,
				Parts:   []*man.Part{},
			}

			if email.CC != nil && len(email.CC) > 0 {
				ccAddr, nil := mail.ParseAddressList(strings.Join(email.CC, ", "))
				if err != nil {
					ccAddr = []*mail.Address{}
					for _, addr := range email.CC {
						ccAddr = append(ccAddr, &mail.Address{
							Address: addr,
						})
					}
				}

				manifest.CC = ccAddr
			}

			// Encrypt and hash the body
			encryptedBody, err := shared.EncryptAndArmor([]byte(email.Body), keyring)
			if err != nil {
				return err
			}
			hash := sha256.Sum256([]byte(email.Body))

			// Append body to the parts
			manifest.Parts = append(manifest.Parts, &man.Part{
				ID:          "body",
				Hash:        hex.EncodeToString(hash[:]),
				ContentType: email.ContentType,
				Size:        len(email.Body),
			})

			// Encrypt the attachments
			for _, file := range files {
				// Encrypt the attachment
				cipher, err := shared.EncryptAndArmor([]byte(file.Data), keyring)
				if err != nil {
					return err
				}

				// Hash it
				hash := sha256.Sum256([]byte(file.Data))

				// Generate a random ID
				id := uniuri.NewLen(20)

				// Push the attachment into the manifest
				manifest.Parts = append(manifest.Parts, &man.Part{
					ID:          id,
					Hash:        hex.EncodeToString(hash[:]),
					Filename:    file.Name,
					ContentType: file.Encoding,
					Size:        len(file.Data),
				})

				// Replace the file in database
				_, err = gorethink.Db(config.RethinkDatabase).Table("files").Get(file.ID).Replace(&models.File{
					Resource: models.Resource{
						ID:           file.ID,
						DateCreated:  file.DateCreated,
						DateModified: time.Now(),
						Name:         id + ".pgp",
						Owner:        account.ID,
					},
					Encrypted: models.Encrypted{
						Encoding: "application/pgp-encrypted",
						Data:     string(cipher),
					},
				}).Run(session)
				if err != nil {
					return err
				}
			}

			// Encrypt the manifest
			strManifest, err := man.Write(manifest)
			if err != nil {
				return err
			}
			encryptedManifest, err := shared.EncryptAndArmor(strManifest, keyring)
			if err != nil {
				return err
			}

			_, err = gorethink.Db(config.RethinkDatabase).Table("emails").Get(email.ID).Replace(&models.Email{
				Resource: models.Resource{
					ID:           email.ID,
					DateCreated:  email.DateCreated,
					DateModified: time.Now(),
					Name:         "Encrypted message (" + email.ID + ")",
					Owner:        account.ID,
				},
				Kind:     "manifest",
				From:     email.From,
				To:       email.To,
				CC:       email.CC,
				BCC:      email.BCC,
				Files:    email.Files,
				Manifest: string(encryptedManifest),
				Body:     string(encryptedBody),
				Thread:   email.Thread,
				Status:   "sent",
			}).Run(session)
			if err != nil {
				return err
			}
		} else if email.Kind == "pgpmime" {
			buffer := &bytes.Buffer{}

			context := &pgpContext{
				From:        ctxFrom,
				CombinedTo:  strings.Join(email.To, ", "),
				MessageID:   messageID,
				Subject:     quotedprintable.EncodeToString([]byte(email.Name)),
				ContentType: email.ContentType,
				Body:        quotedprintable.EncodeToString([]byte(email.Body)),
			}

			if email.CC != nil && len(email.CC) > 0 {
				context.HasCC = true
				context.CombinedCC = strings.Join(email.CC, ", ")
			}

			if email.ReplyTo != "" {
				context.HasReplyTo = true
				context.ReplyTo = email.ReplyTo
			}

			if err := pgpTemplate.Execute(buffer, context); err != nil {
				return err
			}

			contents = buffer.String()
		} else if email.Kind == "manifest" {
			if files == nil || len(files) == 0 {
				buffer := &bytes.Buffer{}

				context := &manifestSingleContext{
					From:        ctxFrom,
					CombinedTo:  strings.Join(email.To, ", "),
					MessageID:   messageID,
					Subject:     quotedprintable.EncodeToString([]byte(email.Name)),
					Boundary1:   uniuri.NewLen(20),
					Boundary2:   uniuri.NewLen(20),
					ID:          email.ID,
					Body:        quotedprintable.EncodeToString([]byte(email.Body)),
					Manifest:    email.Manifest,
					SubjectHash: thread.SubjectHash,
				}

				if email.CC != nil && len(email.CC) > 0 {
					context.HasCC = true
					context.CombinedCC = strings.Join(email.CC, ", ")
				}

				if email.ReplyTo != "" {
					context.HasReplyTo = true
					context.ReplyTo = email.ReplyTo
				}

				if err := manifestSingleTemplate.Execute(buffer, context); err != nil {
					return err
				}

				contents = buffer.String()
			} else {
				buffer := &bytes.Buffer{}

				emailFiles := []*emailFile{}
				for _, file := range files {
					emailFiles = append(emailFiles, &emailFile{
						Encoding: file.Encoding,
						Name:     file.Name,
						Body:     base64.StdEncoding.EncodeToString([]byte(file.Data)),
					})
				}

				context := &manifestMultiContext{
					From:        ctxFrom,
					CombinedTo:  strings.Join(email.To, ", "),
					MessageID:   messageID,
					Subject:     quotedprintable.EncodeToString([]byte(email.Name)),
					Boundary1:   uniuri.NewLen(20),
					Boundary2:   uniuri.NewLen(20),
					ID:          email.ID,
					Body:        quotedprintable.EncodeToString([]byte(email.Body)),
					Manifest:    email.Manifest,
					SubjectHash: thread.SubjectHash,
					Files:       emailFiles,
				}

				if email.CC != nil && len(email.CC) > 0 {
					context.HasCC = true
					context.CombinedCC = strings.Join(email.CC, ", ")
				}

				if email.ReplyTo != "" {
					context.HasReplyTo = true
					context.ReplyTo = email.ReplyTo
				}

				if err := manifestMultiTemplate.Execute(buffer, context); err != nil {
					return err
				}

				contents = buffer.String()
			}
		}

		recipients := email.To
		if email.CC != nil {
			recipients = append(recipients, email.CC...)
		}

		nsqmsg, _ := json.Marshal(map[string]interface{}{
			"id":    email.ID,
			"owner": email.Owner,
		})

		// Sign the email
		if dkimSigner != nil {
			parts := strings.Split(email.From, "@")
			if len(parts) == 2 {
				if _, ok := dkimSigner[parts[1]]; ok {
					// Replace newlines with \r\n
					contents = strings.Replace(contents, "\n", "\r\n", -1)

					// Sign it
					data, err := dkimSigner[parts[1]].Sign([]byte(contents))
					if err != nil {
						log.Print(err)
						return err
					}

					// Replace contents with signed
					contents = strings.Replace(string(data), "\r\n", "\n", -1)
				}
			}
		}

		if err := smtp.SendMail(config.SMTPAddress, nil, email.From, recipients, []byte(contents)); err != nil {
			err := producer.Publish("email_bounced", nsqmsg)
			if err != nil {
				log.WithFields(logrus.Fields{
					"error": err,
				}).Error("Unable to publish a bounce msg")
			}
		} else {

			err := producer.Publish("email_delivery", nsqmsg)
			if err != nil {
				log.WithFields(logrus.Fields{
					"error": err,
				}).Error("Unable to publish a bounce msg")
			}
		}

		msg.Finish()

		return nil
	}), 10)

	if err := consumer.ConnectToNSQLookupd(config.LookupdAddress); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Unable to connect to nsqlookupd")
	}

	log.Info("Connected to NSQ and awaiting data")
}

/*func ResolveDomain(domain string) (string, error) {
	m := dns.Msg{}
	m.SetQuestion(domain+".", dns.TypeMX)
	m.RecursionDesired = true

	r, _, err := dns.Client{}.Exchange(m, "8.8.8.8:53")
	if err != nil {
		return nil, err
	}

	if r.Rcode != dns.RCodeSuccess {
		return fmt.Errorf("DNS query did not succaeed, code %d", r.Rcode)
	}

	for _, a := range r.Answer {
		if mx, ok := a.(*dns.MX); ok {

		}
	}
}*/
