package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/blang/semver"
	"github.com/dancannon/gorethink"
	"github.com/dchest/uniuri"
	"github.com/lavab/api/models"
	man "github.com/lavab/pgp-manifest-go"
	"github.com/lavab/smtpd"
	"golang.org/x/crypto/openpgp"
)

var domains = map[string]struct{}{
	"lavaboom.com": struct{}{},
	"lavaboom.io":  struct{}{},
	"lavaboom.co":  struct{}{},
}

func PrepareHandler(config *Flags) func(peer smtpd.Peer, env smtpd.Envelope) error {
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
				recipients = append(recipients, parts[0])
			}
		}

		log.Debug("Parsed recipients")

		// If we didn't find a recipient, return an error
		if len(recipients) == 0 {
			return fmt.Errorf("Not supported email domain")
		}

		// Fetch accounts
		cursor, err := gorethink.Db(config.RethinkDatabase).Table("accounts").GetAllByIndex("name", recipients...).Run(session)
		if err != nil {
			return err
		}
		var accounts []*models.Account
		if err := cursor.All(&accounts); err != nil {
			return err
		}

		// Compare request and result lengths
		if len(accounts) != len(recipients) {
			return fmt.Errorf("Email address not found")
		}

		log.Debug("Recipient found")

		// Prepare a variable for the combined keyring of recipients
		toKeyring := []*openpgp.Entity{}

		// Fetch users' public keys
		for _, account := range accounts {
			if account.PublicKey != "" {
				cursor, err := gorethink.Db(config.RethinkDatabase).Table("keys").Get(account.PublicKey).Run(session)
				if err != nil {
					return err
				}

				var key *models.Key
				if err := cursor.One(&key); err != nil {
					return err
				}

				keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key.Key))
				if err != nil {
					return err
				}

				account.Key = keyring[0]
				toKeyring = append(toKeyring, account.Key)
			} else {
				cursor, err := gorethink.Db(config.RethinkDatabase).Table("keys").GetAllByIndex("owner", account.ID).Run(session)
				if err != nil {
					return err
				}

				var keys []*models.Key
				if err := cursor.All(&keys); err != nil {
					return err
				}

				if len(keys) == 0 {
					return fmt.Errorf("Recipient has no public key")
				}

				keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(keys[0].Key))
				if err != nil {
					return err
				}

				account.Key = keyring[0]
				toKeyring = append(toKeyring, account.Key)
			}
		}

		log.Debug("Fetched keys")

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
				if strings.HasPrefix(child.Headers.Get("Content-Type"), "application/x-pgp-panifest") {
					kind = "manifest"
					break
				}
			}
		}

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
					firstIndex := -1

					// Find the first supported body
					for index, child := range msg.Children {
						contentType := child.Headers.Get("Content-Type")
						if strings.HasPrefix(contentType, "application/pgp-encrypted") ||
							strings.HasPrefix(contentType, "text/html") ||
							strings.HasPrefix(contentType, "text/plain") {
							firstIndex = index
							break
						}
					}

					// Parse its media type to remove non-required stuff
					match := msg.Children[firstIndex]
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
						encryptedBody, err := encryptAndArmor(msg.Body, toKeyring)
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
				return err
			}
			to, err := email.Headers.AddressList("from")
			if err != nil {
				return err
			}
			cc, err := email.Headers.AddressList("from")
			if err != nil {
				return err
			}

			// Generate the manifest
			emailID := uniuri.NewLen(uniuri.UUIDLen)
			subject = "Encrypted message (" + emailID + ")"
			rawManifest := &man.Manifest{
				Version: semver.Version{1, 0, 0, nil, nil},
				From:    from[0],
				To:      to,
				CC:      cc,
				Subject: email.Headers.Get("subject"),
				Parts:   parts,
			}

			// Encrypt the manifest and the body
			encryptedBody, err := encryptAndArmor([]byte(bodyText), toKeyring)
			if err != nil {
				return err
			}
			strManifest, err := man.Write(rawManifest)
			if err != nil {
				return err
			}
			encryptedManifest, err := encryptAndArmor(strManifest, toKeyring)
			if err != nil {
				return err
			}

			body = string(encryptedBody)
			manifest = string(encryptedManifest)

			_ = subject
		} else if kind == "manifest" {
			// Variables used for attachment search
			manifestIndex := -1
			bodyIndex := -1

			// Find indexes of the manifest and the body
			for index, child := range email.Children {
				contentType := child.Headers.Get("Content-Type")

				if strings.Index(contentType, "application/x-pgp-encrypted") == 0 {
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
					break
				}
			}
		}

		// Debug
		fmt.Printf("%v", body)
		fmt.Printf("%v", manifest)

		return nil
	}
}
