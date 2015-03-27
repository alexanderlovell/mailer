package manifest

import (
	"encoding/json"
	"strings"
)

func clearAddress(input string) string {
	if input[0] == '<' {
		return strings.Trim(input, "<> ")
	}

	return strings.TrimSpace(input)
}

func Write(input *Manifest) ([]byte, error) {
	if input.Headers == nil {
		input.Headers = map[string]string{}
	} else {
		for key, value := range input.Headers {
			input.Headers[key] = strings.ToLower(value)
		}
	}

	if input.From != nil {
		input.Headers["from"] = clearAddress(input.From.Name + " <" + input.From.Address + ">")
	}

	if input.To != nil {
		to := []string{}
		for _, addr := range input.To {
			to = append(to, clearAddress(addr.Name+" <"+addr.Address+">"))
		}

		input.Headers["to"] = strings.Join(to, ", ")
	}

	if input.CC != nil {
		cc := []string{}
		for _, addr := range input.CC {
			cc = append(cc, clearAddress(addr.Name+" <"+addr.Address+">"))
		}

		input.Headers["cc"] = strings.Join(cc, ", ")
	}

	if input.Subject != "" {
		input.Headers["subject"] = input.Subject
	}

	if input.ContentType != "" {
		input.Headers["content-type"] = input.ContentType
	}

	return json.Marshal(input)
}
