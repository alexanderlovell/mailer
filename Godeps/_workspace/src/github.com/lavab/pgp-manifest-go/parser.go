package manifest

import (
	"encoding/json"
	"net/mail"
)

func Parse(input []byte) (*Manifest, error) {
	// Prepare a new manifest struct
	var manifest Manifest

	// Decode the input
	if err := json.Unmarshal(input, &manifest); err != nil {
		return nil, err
	}

	// Move the headers into the main manifest struct
	if val, ok := manifest.Headers["from"]; ok {
		from, err := mail.ParseAddress(val)
		if err != nil {
			return nil, err
		}

		manifest.From = from
	}
	if val, ok := manifest.Headers["to"]; ok {
		to, err := mail.ParseAddressList(val)
		if err != nil {
			return nil, err
		}

		manifest.To = to
	}
	if val, ok := manifest.Headers["cc"]; ok {
		cc, err := mail.ParseAddressList(val)
		if err != nil {
			return nil, err
		}

		manifest.CC = cc
	}
	if val, ok := manifest.Headers["subject"]; ok {
		manifest.Subject = val
	}
	if val, ok := manifest.Headers["content-type"]; ok {
		manifest.ContentType = val
	}

	return &manifest, nil
}
