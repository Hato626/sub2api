package service

import (
	"bytes"
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	codexEnvironmentTimezonePattern = regexp.MustCompile(`<timezone>[^<]*</timezone>`)
	codexEnvironmentDatePattern     = regexp.MustCompile(`<current_date>\d{4}-\d{2}-\d{2}</current_date>`)
)

func (s *OpenAIGatewayService) rewriteCodexTimezoneIfEnabled(ctx context.Context, account *Account, body []byte) []byte {
	if s == nil || s.settingService == nil || account == nil || !account.IsOpenAI() || !bytes.Contains(body, []byte("environment_context")) {
		return body
	}
	policy := s.settingService.GetCodexTimezoneRewriteSettings(ctx)
	if !policy.Enabled {
		return body
	}
	rewritten, _ := rewriteCodexEnvironmentTimezone(body, policy.location, time.Now())
	return rewritten
}

// rewriteCodexEnvironmentTimezone patches only the newest user input_text environment block.
// Historical dates, unknown JSON fields, tool data and the caller's original buffer stay intact.
// gjson decodes text before matching, including WS bridge bodies with \u003c escaping;
// sjson replaces one string without unmarshalling images and the entire conversation into objects.
func rewriteCodexEnvironmentTimezone(body []byte, location *time.Location, now time.Time) ([]byte, bool) {
	if location == nil || !gjson.ValidBytes(body) {
		return body, false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false
	}
	var text, path string
	input.ForEach(func(i, item gjson.Result) bool {
		if item.Get("role").String() != "user" {
			return true
		}
		content := item.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(j, part gjson.Result) bool {
			if part.Get("type").String() != "input_text" {
				return true
			}
			value := part.Get("text")
			if value.Type != gjson.String || !strings.HasPrefix(strings.TrimSpace(value.Str), "<environment_context>") || !strings.Contains(value.Str, "</environment_context>") {
				return true
			}
			text = value.Str
			path = "input." + strconv.FormatInt(i.Int(), 10) + ".content." + strconv.FormatInt(j.Int(), 10) + ".text"
			return true
		})
		return true
	})
	if path == "" {
		return body, false
	}
	end := strings.Index(text, "</environment_context>")
	block := text[:end]
	replaceFirst := func(pattern *regexp.Regexp, replacement string) {
		if match := pattern.FindStringIndex(block); match != nil {
			block = block[:match[0]] + replacement + block[match[1]:]
		}
	}
	replaceFirst(codexEnvironmentTimezonePattern, "<timezone>"+location.String()+"</timezone>")
	replaceFirst(codexEnvironmentDatePattern, "<current_date>"+now.In(location).Format("2006-01-02")+"</current_date>")
	updated := block + text[end:]
	if updated == text {
		return body, false
	}
	out, err := sjson.SetBytes(body, path, updated)
	if err != nil {
		return body, false
	}
	return out, true
}
