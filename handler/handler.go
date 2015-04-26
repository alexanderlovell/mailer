package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/alexcesaro/quotedprintable"
	"github.com/bitly/go-nsq"
	"github.com/blang/semver"
	"github.com/dancannon/gorethink"
	"github.com/dchest/uniuri"
	"github.com/lavab/api/models"
	"github.com/lavab/api/utils"
	"github.com/lavab/mailer/shared"
	man "github.com/lavab/pgp-manifest-go"
	"github.com/lavab/smtpd"
	"github.com/saintienn/go-spamc"
	"golang.org/x/crypto/openpgp"
)

var domains = map[string]struct{}{
	"lavaboom.com": struct{}{},
	"lavaboom.io":  struct{}{},
	"lavaboom.co":  struct{}{},
}

var (
	cfg     *shared.Flags
	session *gorethink.Session
)

func PrepareHandler(config *shared.Flags) func(peer smtpd.Peer, env smtpd.Envelope) error {
	cfg = config

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
	var err error
	session, err = gorethink.Connect(gorethink.ConnectOpts{
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

	// Connect to NSQ
	producer, err := nsq.NewProducer(config.NSQDAddress, nsq.NewConfig())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatal("Unable to connect to NSQd")
	}

	// Create a new spamd client
	spam := spamc.New(config.SpamdAddress, 10)

	// Last message sent by PrepareHandler
	log.WithFields(logrus.Fields{
		"addr": config.BindAddress,
	}).Info("Listening for incoming traffic")

	return func(peer smtpd.Peer, e smtpd.Envelope) error {
		log.Debug("Started parsing")

		// Check recipients for Lavaboom users
		recipients := []interface{}{}
		for _, recipient := range e.Recipients {
			// Split the email address into username and domain
			parts := strings.Split(recipient, "@")
			if len(parts) != 2 {
				return fmt.Errorf("Invalid recipient email address")
			}

			// Check if we support that domain
			if _, ok := domains[parts[1]]; ok {
				recipients = append(recipients,
					utils.RemoveDots(
						utils.NormalizeUsername(parts[0]),
					),
				)
			}
		}

		log.Debug("Parsed recipients")

		// If we didn't find a recipient, return an error
		if len(recipients) == 0 {
			return fmt.Errorf("Not supported email domain")
		}

		// Fetch the mapping
		cursor, err := gorethink.Db(config.RethinkDatabase).Table("addresses").GetAll(recipients...).Run(session)
		if err != nil {
			return err
		}
		var addresses []*models.Address
		if err := cursor.All(&addresses); err != nil {
			return err
		}

		// Transform the mapping into accounts
		accountIDs := []interface{}{}
		for _, address := range addresses {
			accountIDs = append(accountIDs, address.Owner)
		}

		// Fetch accounts
		cursor, err = gorethink.Db(config.RethinkDatabase).Table("accounts").GetAll(accountIDs...).Run(session)
		if err != nil {
			return err
		}
		var accounts []*models.Account
		if err := cursor.All(&accounts); err != nil {
			return err
		}

		// Compare request and result lengths
		if len(accounts) != len(recipients) {
			return fmt.Errorf("One of the email addresses wasn't found")
		}

		log.Debug("Recipients found")

		// Prepare a variable for the combined keyring of recipients
		toKeyring := []*openpgp.Entity{}

		// Fetch users' public keys
		for _, account := range accounts {
			account.Key, err = getAccountPublicKey(account)
			if err != nil {
				return err
			}

			toKeyring = append(toKeyring, account.Key)
		}

		log.Debug("Fetched keys")

		// Check in the antispam
		isSpam := false
		spamReply, err := spam.Report(string(e.Data))
		if err == nil {
			log.Print(spamReply.Code)
			log.Print(spamReply.Message)
			log.Print(spamReply.Vars)
		}
		if spamReply.Code == spamc.EX_OK {
			log.Print("Proper code")
			if spam, ok := spamReply.Vars["isSpam"]; ok && spam.(bool) {
				log.Print("It's spam.")
				isSpam = true
			}
		}

		// Parse the email
		email, err := ParseEmail(bytes.NewReader(e.Data))
		if err != nil {
			return err
		}

		// Determine email's kind
		contentType := email.Headers.Get("Content-Type")
		kind := "raw"
		if strings.HasPrefix(contentType, "multipart/encrypted") {
			// multipart/encrypted is dedicated for PGP/MIME and S/MIME
			kind = "pgpmime"
		} else if strings.HasPrefix(contentType, "multipart/mixed") && len(email.Children) >= 2 {
			// Has manifest? It is an email with a PGP manifest. If not, it's unencrypted.
			for _, child := range email.Children {
				if strings.HasPrefix(child.Headers.Get("Content-Type"), "application/x-pgp-manifest") {
					kind = "manifest"
					break
				}
			}
		}

		// Copy kind to a second variable for later parsing
		initialKind := kind

		// Debug the kind
		log.Debugf("Email is %s", kind)

		// Declare variables used later for data insertion
		var (
			subject  string
			manifest string
			body     string
			fileIDs  = map[string][]string{}
			files    = []*models.File{}
		)

		// Transform raw emails into encrypted with manifests
		if kind == "raw" {
			// Prepare variables for manifest generation
			parts := []*man.Part{}

			// Parsing vars
			var (
				bodyType string
				bodyText string
			)

			// Flatten the email
			var parseBody func(msg *Message) error
			parseBody = func(msg *Message) error {
				contentType := msg.Headers.Get("Content-Type")

				if strings.HasPrefix(contentType, "multipart/alternative") {
					preferredType := ""
					preferredIndex := -1

					// Find the best body
					for index, child := range msg.Children {
						contentType := child.Headers.Get("Content-Type")
						if strings.HasPrefix(contentType, "application/pgp-encrypted") {
							preferredType = "pgp"
							preferredIndex = index
							break
						}

						if strings.HasPrefix(contentType, "text/html") {
							preferredType = "html"
							preferredIndex = index
						}

						if strings.HasPrefix(contentType, "text/plain") {
							if preferredType != "html" {
								preferredType = "plain"
								preferredIndex = index
							}
						}
					}

					// Parse its media type to remove non-required stuff
					match := msg.Children[preferredIndex]
					mediaType, _, err := mime.ParseMediaType(match.Headers.Get("Content-Type"))
					if err != nil {
						return err
					}

					// Push contents into the parser's scope
					bodyType = mediaType
					bodyText = string(match.Body)

					/* change of plans - discard them.
					// Transform rest of the types into attachments
					nodeID := uniuri.New()
					for _, child := range msg.Children {
						child.Headers["disposition"] = "attachment; filename=\"alternative." + nodeID + "." + mime. +"\""
					}*/
				} else if strings.HasPrefix(contentType, "multipart/") {
					// Tread every other multipart as multipart/mixed, as we parse multipart/encrypted later
					for _, child := range msg.Children {
						if err := parseBody(child); err != nil {
							return err
						}
					}
				} else {
					// Parse the content type
					mediaType, _, err := mime.ParseMediaType(contentType)
					if err != nil {
						return err
					}

					// Not multipart, parse the disposition
					disposition, dparams, err := mime.ParseMediaType(msg.Headers.Get("Content-Disposition"))

					if err == nil && disposition == "attachment" {
						// We're dealing with an attachment
						id := uniuri.NewLen(uniuri.UUIDLen)

						// Encrypt the body
						encryptedBody, err := shared.EncryptAndArmor(msg.Body, toKeyring)
						if err != nil {
							return err
						}

						// Hash the body
						rawHash := sha256.Sum256(msg.Body)
						hash := hex.EncodeToString(rawHash[:])

						// Push the attachment into parser's scope
						parts = append(parts, &man.Part{
							Hash:        hash,
							ID:          id,
							ContentType: mediaType,
							Filename:    dparams["filename"],
							Size:        len(msg.Body),
						})

						for _, account := range accounts {
							fid := uniuri.NewLen(uniuri.UUIDLen)

							files = append(files, &models.File{
								Resource: models.Resource{
									ID:           fid,
									DateCreated:  time.Now(),
									DateModified: time.Now(),
									Name:         id + ".pgp",
									Owner:        account.ID,
								},
								Encrypted: models.Encrypted{
									Encoding: "application/pgp-encrypted",
									Data:     string(encryptedBody),
								},
							})

							if _, ok := fileIDs[account.ID]; !ok {
								fileIDs[account.ID] = []string{}
							}

							fileIDs[account.ID] = append(fileIDs[account.ID], fid)
						}
					} else {
						// Header is either corrupted or we're dealing with inline
						if bodyType == "" && mediaType == "text/plain" || mediaType == "text/html" {
							bodyType = mediaType
							bodyText = string(msg.Body)
						} else if bodyType == "" {
							bodyType = "text/html"

							if strings.Index(mediaType, "image/") == 0 {
								bodyText = `<img src="data:` + mediaType + `;base64,` + base64.StdEncoding.EncodeToString(msg.Body) + `"><br>`
							} else {
								bodyText = "<pre>" + string(msg.Body) + "</pre>"
							}
						} else if mediaType == "text/plain" {
							if bodyType == "text/plain" {
								bodyText += "\n\n" + string(msg.Body)
							} else {
								bodyText += "\n\n<pre>" + string(msg.Body) + "</pre>"
							}
						} else if mediaType == "text/html" {
							if bodyType == "text/plain" {
								bodyType = "text/html"
								bodyText = "<pre>" + bodyText + "</pre>\n\n" + string(msg.Body)
							} else {
								bodyText += "\n\n" + string(msg.Body)
							}
						} else {
							if bodyType != "text/html" {
								bodyType = "text/html"
								bodyText = "<pre>" + bodyText + "</pre>"
							}

							// Put images as HTML tags
							if strings.Index(mediaType, "image/") == 0 {
								bodyText = "\n\n<img src=\"data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(msg.Body) + "\"><br>"
							} else {
								bodyText = "\n\n<pre>" + string(msg.Body) + "</pre>"
							}
						}
					}
				}

				return nil
			}

			// Parse the email
			parseBody(email)

			// Trim the body text
			bodyText = strings.TrimSpace(bodyText)

			// Hash the body
			bodyHash := sha256.Sum256([]byte(bodyText))

			// Append body to the parts
			parts = append(parts, &man.Part{
				Hash:        hex.EncodeToString(bodyHash[:]),
				ID:          "body",
				ContentType: bodyType,
				Size:        len(bodyText),
			})

			// Debug info
			log.Debug("Finished parsing the email")

			// Push files into RethinkDB
			for _, file := range files {
				_, err := gorethink.Db(config.RethinkDatabase).Table("files").Insert(file).Run(session)
				if err != nil {
					return err
				}
			}

			// Generate the from, to and cc addresses
			from, err := email.Headers.AddressList("from")
			if err != nil {
				from = []*mail.Address{}
			}
			to, err := email.Headers.AddressList("to")
			if err != nil {
				to = []*mail.Address{}
			}
			cc, err := email.Headers.AddressList("cc")
			if err != nil {
				cc = []*mail.Address{}
			}

			// Generate the manifest
			emailID := uniuri.NewLen(uniuri.UUIDLen)
			subject = "Encrypted message (" + emailID + ")"

			s2 := email.Headers.Get("subject")
			if len(s2) > 1 && s2[0] == '=' && s2[1] == '?' {
				s2, _, err = quotedprintable.DecodeHeader(s2)
				if err != nil {
					return err
				}
			}

			var fm *mail.Address
			if len(from) > 0 {
				fm = from[0]
			} else {
				fm = "no from header"
			}
			rawManifest := &man.Manifest{
				Version: semver.Version{1, 0, 0, nil, nil},
				From:    fm,
				To:      to,
				CC:      cc,
				Subject: s2,
				Parts:   parts,
			}

			// Encrypt the manifest and the body
			encryptedBody, err := shared.EncryptAndArmor([]byte(bodyText), toKeyring)
			if err != nil {
				return err
			}
			strManifest, err := man.Write(rawManifest)
			if err != nil {
				return err
			}
			encryptedManifest, err := shared.EncryptAndArmor(strManifest, toKeyring)
			if err != nil {
				return err
			}

			body = string(encryptedBody)
			manifest = string(encryptedManifest)
			kind = "manifest"

			_ = subject
		} else if kind == "manifest" {
			// Variables used for attachment search
			manifestIndex := -1
			bodyIndex := -1

			// Find indexes of the manifest and the body
			for index, child := range email.Children {
				contentType := child.Headers.Get("Content-Type")

				if strings.Index(contentType, "application/x-pgp-manifest") == 0 {
					manifestIndex = index
				} else if strings.Index(contentType, "multipart/alternative") == 0 {
					bodyIndex = index
				}

				if manifestIndex != -1 && bodyIndex != -1 {
					break
				}
			}

			// Check that we found both parts
			if manifestIndex == -1 || bodyIndex == -1 {
				return fmt.Errorf("Invalid PGP/Manifest email")
			}

			// Search for the body child index
			bodyChildIndex := -1
			for index, child := range email.Children[bodyIndex].Children {
				contentType := child.Headers.Get("Content-Type")

				if strings.Index(contentType, "application/pgp-encrypted") == 0 {
					bodyChildIndex = index
					break
				}
			}

			// Check that we found it
			if bodyChildIndex == -1 {
				return fmt.Errorf("Invalid PGP/Manifest email body")
			}

			// Find the manifest and the body
			manifest = string(email.Children[manifestIndex].Body)
			body = string(email.Children[bodyIndex].Children[bodyChildIndex].Body)
			subject = "Encrypted email"

			// Gather attachments and insert them into db
			for index, child := range email.Children {
				if index == bodyIndex || index == manifestIndex {
					continue
				}

				_, cdparams, err := mime.ParseMediaType(child.Headers.Get("Content-Disposition"))
				if err != nil {
					return err
				}

				for _, account := range accounts {
					fid := uniuri.NewLen(uniuri.UUIDLen)

					_, err := gorethink.Db(config.RethinkDatabase).Table("files").Insert(&models.File{
						Resource: models.Resource{
							ID:           fid,
							DateCreated:  time.Now(),
							DateModified: time.Now(),
							Name:         cdparams["filename"],
							Owner:        account.ID,
						},
						Encrypted: models.Encrypted{
							Encoding: "application/pgp-encrypted",
							Data:     string(child.Body),
						},
					}).Run(session)
					if err != nil {
						return err
					}

					if _, ok := fileIDs[account.ID]; !ok {
						fileIDs[account.ID] = []string{}
					}

					fileIDs[account.ID] = append(fileIDs[account.ID], fid)
				}
			}
		} else if kind == "pgpmime" {
			for _, child := range email.Children {
				if strings.Index(child.Headers.Get("Content-Type"), "application/pgp-encrypted") != -1 {
					body = string(child.Body)
					subject = child.Headers.Get("Subject")
					break
				}
			}
		}

		if subject[0] == '=' && subject[1] == '?' {
			subject, _, err = quotedprintable.DecodeHeader(subject)
			if err != nil {
				return err
			}
		}

		// Save the email for each recipient
		for _, account := range accounts {
			// Find user's Inbox label
			cursor, err := gorethink.Db(config.RethinkDatabase).Table("labels").Filter(map[string]interface{}{
				"owner":   account.ID,
				"name":    "Inbox",
				"builtin": true,
			}).Run(session)
			if err != nil {
				return err
			}
			var inbox *models.Label
			if err := cursor.One(&inbox); err != nil {
				return err
			}

			// Find user's Spam label
			cursor, err = gorethink.Db(config.RethinkDatabase).Table("labels").Filter(map[string]interface{}{
				"owner":   account.ID,
				"name":    "Spam",
				"builtin": true,
			}).Run(session)
			if err != nil {
				return err
			}
			var spam *models.Label
			if err := cursor.One(&spam); err != nil {
				return err
			}

			// Get the subject's hash
			subjectHash := email.Headers.Get("Subject-Hash")
			if subjectHash == "" {
				subject := email.Headers.Get("Subject")
				if subject[0] == '=' && subject[1] == '?' {
					subject, _, err = quotedprintable.DecodeHeader(subject)
					if err != nil {
						return err
					}
				}

				subject = shared.StripPrefixes(strings.TrimSpace(subject))

				hash := sha256.Sum256([]byte(subject))
				subjectHash = hex.EncodeToString(hash[:])
			}

			// Generate the email ID
			eid := uniuri.NewLen(uniuri.UUIDLen)

			// Prepare from, to and cc
			from := email.Headers.Get("from")
			if f1, err := email.Headers.AddressList("from"); err == nil && len(f1) > 0 {
				from = strings.TrimSpace(f1[0].Name + " <" + f1[0].Address + ">")
			}
			to := strings.Split(email.Headers.Get("to"), ", ")
			cc := strings.Split(email.Headers.Get("cc"), ", ")
			for i, v := range to {
				to[i] = strings.TrimSpace(v)
			}
			for i, v := range cc {
				cc[i] = strings.TrimSpace(v)
			}

			if len(cc) == 1 && cc[0] == "" {
				cc = nil
			}

			// Transform headers into map[string]string
			fh := map[string]string{}
			for key, values := range email.Headers {
				fh[key] = strings.Join(values, ", ")
			}

			// Find the thread
			var thread *models.Thread

			// First check if either in-reply-to or references headers are set
			irt := ""
			if x := email.Headers.Get("In-Reply-To"); x != "" {
				irt = x
			} else if x := email.Headers.Get("References"); x != "" {
				irt = x
			}

			if irt != "" {
				// Per http://www.jwz.org/doc/threading.html:
				// You can safely assume that the first string between <> in In-Reply-To
				// is the message ID.
				x1i := strings.Index(irt, "<")
				if x1i != -1 {
					x2i := strings.Index(irt[x1i+1:], ">")
					if x2i != -1 {
						irt = irt[x1i+1 : x1i+x2i+1]
					}
				}

				// Look up the parent
				cursor, err := gorethink.Db(config.RethinkDatabase).Table("emails").GetAllByIndex("messageIDOwner", []interface{}{
					irt,
					account.ID,
				}).Run(session)
				if err != nil {
					return err
				}
				var emails []*models.Email
				if err := cursor.All(&emails); err != nil {
					return err
				}

				// Found one = that one is correct
				if len(emails) == 1 {
					cursor, err := gorethink.Db(config.RethinkDatabase).Table("threads").Get(emails[0].ID).Run(session)
					if err != nil {
						return err
					}
					if err := cursor.One(&thread); err != nil {
						return err
					}
				}
			}

			if thread == nil {
				// Match by subject
				cursor, err := gorethink.Db(config.RethinkDatabase).Table("threads").GetAllByIndex("subjectOwner", []interface{}{
					subjectHash,
					account.ID,
				}).Run(session)
				if err != nil {
					return err
				}
				var threads []*models.Thread
				if err := cursor.All(&threads); err != nil {
					return err
				}

				if len(threads) > 0 {
					thread = threads[0]
				}
			}

			if thread == nil {
				secure := "all"
				if initialKind == "raw" {
					secure = "none"
				}

				var label string
				if isSpam {
					label = spam.ID
				} else {
					label = inbox.ID
				}

				thread = &models.Thread{
					Resource: models.Resource{
						ID:           uniuri.NewLen(uniuri.UUIDLen),
						DateCreated:  time.Now(),
						DateModified: time.Now(),
						Name:         "Encrypted thread",
						Owner:        account.ID,
					},
					Emails:      []string{eid},
					Labels:      []string{label},
					Members:     append(append(to, cc...), from),
					IsRead:      false,
					SubjectHash: subjectHash,
					Secure:      secure,
				}

				_, err := gorethink.Db(config.RethinkDatabase).Table("threads").Insert(thread).Run(session)
				if err != nil {
					return err
				}
			} else {
				var desiredID string
				if isSpam {
					desiredID = spam.ID
				} else {
					desiredID = inbox.ID
				}

				foundLabel := false
				for _, label := range thread.Labels {
					if label == desiredID {
						foundLabel = true
						break
					}
				}
				if !foundLabel {
					thread.Labels = append(thread.Labels, desiredID)
				}

				thread.Emails = append(thread.Emails, eid)

				update := map[string]interface{}{
					"date_modified": gorethink.Now(),
					"is_read":       false,
					"labels":        thread.Labels,
					"emails":        thread.Emails,
				}

				// update thread.secure depending on email's kind
				if (initialKind == "raw" && thread.Secure == "all") ||
					(initialKind == "manifest" && thread.Secure == "none") ||
					(initialKind == "pgpmime" && thread.Secure == "none") {
					update["secure"] = "some"
				}

				_, err := gorethink.Db(config.RethinkDatabase).Table("threads").Get(thread.ID).Update(update).Run(session)
				if err != nil {
					return err
				}
			}

			// Generate list of all owned emails
			ownEmails := map[string]struct{}{}
			for domain, _ := range domains {
				ownEmails[account.Name+"@"+domain] = struct{}{}
			}

			// Remove ownEmails from to and cc
			to2 := []string{}
			for _, value := range to {
				addr, err := mail.ParseAddress(value)
				if err != nil {
					// Mail is probably empty
					continue
				}

				if _, ok := ownEmails[addr.Address]; !ok {
					to2 = append(to2, value)
				}
			}

			to = to2

			if cc != nil {
				cc2 := []string{}
				for _, value := range cc {
					addr, err := mail.ParseAddress(value)
					if err != nil {
						continue
					}

					if _, ok := ownEmails[addr.Address]; !ok {
						cc2 = append(cc2, value)
					}
				}

				cc = cc2
			}

			// Prepare a new email
			es := &models.Email{
				Resource: models.Resource{
					ID:           eid,
					DateCreated:  time.Now(),
					DateModified: time.Now(),
					Name:         subject,
					Owner:        account.ID,
				},
				Kind:      kind,
				From:      from,
				To:        to,
				CC:        cc,
				Body:      body,
				Thread:    thread.ID,
				MessageID: strings.Trim(email.Headers.Get("Message-ID"), "<>"),
				Status:    "received",
			}

			if fileIDs != nil {
				es.Files = fileIDs[account.ID]
			}

			if manifest != "" {
				es.Manifest = manifest
			}

			// Insert the email
			_, err = gorethink.Db(config.RethinkDatabase).Table("emails").Insert(es).Run(session)
			if err != nil {
				return err
			}

			// Prepare a notification message
			notification, err := json.Marshal(map[string]interface{}{
				"id":    eid,
				"owner": account.ID,
			})
			if err != nil {
				return err
			}

			// Notify the cluster
			err = producer.Publish("email_receipt", notification)
			if err != nil {
				return err
			}

			log.WithFields(logrus.Fields{
				"id": eid,
			}).Info("Finished processing an email")
		}

		return nil
	}
}

func getAccountPublicKey(account *models.Account) (*openpgp.Entity, error) {
	if account.PublicKey != "" {
		cursor, err := gorethink.Db(cfg.RethinkDatabase).Table("keys").Get(account.PublicKey).Run(session)
		if err != nil {
			return nil, err
		}

		var key *models.Key
		if err := cursor.One(&key); err != nil {
			return nil, err
		}

		keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key.Key))
		if err != nil {
			return nil, err
		}

		return keyring[0], nil
	}
	cursor, err := gorethink.Db(cfg.RethinkDatabase).Table("keys").GetAllByIndex("owner", account.ID).Run(session)
	if err != nil {
		return nil, err
	}

	var keys []*models.Key
	if err := cursor.All(&keys); err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("Recipient has no public key")
	}

	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(keys[0].Key))
	if err != nil {
		return nil, err
	}

	return keyring[0], nil
}
