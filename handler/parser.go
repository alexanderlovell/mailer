package handler

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

type Message struct {
	Headers  mail.Header
	Body     []byte
	Children []*Message
}

func ParseEmail(input io.Reader) (*Message, error) {
	// Create a new mail reader
	r1, err := mail.ReadMessage(input)
	if err != nil {
		return nil, err
	}

	// Allocate an email struct
	message := &Message{}
	message.Headers = r1.Header

	// Default Content-Type is text/plain
	if ct := message.Headers.Get("Content-Type"); ct == "" {
		message.Headers["Content-Type"] = []string{"text/plain"}
	}

	// Determine the content type - fetch it and parse it
	log.Print(message.Headers.Get("content-type"))
	mediaType, params, err := mime.ParseMediaType(message.Headers.Get("content-type"))
	if err != nil {
		return nil, err
	}

	// If the email is not multipart, finish the struct and return
	if !strings.HasPrefix(mediaType, "multipart/") {
		message.Body, err = ioutil.ReadAll(r1.Body)
		if err != nil {
			return nil, err
		}

		return message, nil
	}

	// Ensure thet a boundary was passed
	if _, ok := params["boundary"]; !ok {
		return nil, errors.New("No boundary passed")
	}

	// Prepare a slice for children
	message.Children = []*Message{}

	// Create a new multipart reader
	r2 := multipart.NewReader(r1.Body, params["boundary"])

	// Parse all children
	for {
		// Get the next part
		part, err := r2.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		// Convert the headers back into a byte slice
		header := []byte{}
		for key, values := range part.Header {
			header = append(header, []byte(key+": "+strings.Join(values, ", "))...)
		}

		// Read the body - awful thing to do
		body, err := ioutil.ReadAll(part)
		if err != nil {
			return nil, err
		}

		// Merge headers and body and pass it into ParseEmail
		parsed, err := ParseEmail(
			bytes.NewReader(
				append(append(header, '\n', '\n'), body...),
			),
		)
		if err != nil {
			return nil, err
		}

		// Put the child into parent struct
		message.Children = append(message.Children, parsed)
	}

	// Return the parsed email
	return message, nil
}
