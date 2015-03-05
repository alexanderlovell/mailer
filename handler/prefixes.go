package handler

import (
	"regexp"
)

var prefixRegex = regexp.MustCompile(`(?i)([\[\(] *)?(RE?S?|FYI|RIF|I|FS|VB|RV|ENC|ODP|PD|YNT|ILT|SV|VS|VL|AW|WG|ΑΠ|ΣΧΕΤ|ΠΡΘ|תגובה|הועבר|主题|转发|FWD?) *([-:;)\]][ :;\])-]*|$)|\]+ *$`)

func StripPrefixes(input string) string {
	return prefixRegex.ReplaceAllString(input, "")
}
