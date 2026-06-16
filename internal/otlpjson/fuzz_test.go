package otlpjson

import (
	"bytes"
	"regexp"
	"testing"
)

// FuzzDecodeNoPanic pins that the OTLP-JSON ingestion boundary never panics on
// arbitrary bytes: untrusted capture payloads must be REFUSED with an error, not
// crash the tool (CLAUDE.md fail-closed). The return value is ignored; the
// contract under test is "error, never panic".
func FuzzDecodeNoPanic(f *testing.F) {
	for _, s := range []string{
		``, `{}`, `null`, `[`, `[]`, `{"resourceSpans":[]}`,
		`{"resourceSpans":[{"scopeSpans":[{"spans":[{"spanId":"","name":"x"}]}]}]}`,
		`42`, `"x"`, `{"resource_spans":[{"scope_spans":[{"spans":[{}]}]}]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(bytes.NewReader(data)) // must not panic
	})
}

var canonicalInt = regexp.MustCompile(`^-?(0|[1-9]\d*)$`)

// FuzzIntStrFailsClosed pins the int-attribute invariant: anyValue.str() for an
// intValue returns EITHER "" (refused) OR a canonical decimal int64 — never a
// half-validated garbage string that would leak into an op key. This makes the
// fail-closed property of the int path self-checking against future edits.
func FuzzIntStrFailsClosed(f *testing.F) {
	for _, s := range []string{`"123"`, `123`, `"-7"`, `null`, `"1\"2"`, `"abc"`, `""`, `999999999999999999999`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := anyValue{IntValue: []byte(raw)}.str()
		if got != "" && !canonicalInt.MatchString(got) {
			t.Errorf("intValue=%q produced non-canonical, non-empty %q (fail-open)", raw, got)
		}
	})
}
