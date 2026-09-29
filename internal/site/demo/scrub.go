package demo

import (
	"fmt"
	"reflect"
	"regexp"
)

// leakRe is what must never reach the page from a take: local paths, which
// name the account and the machine; e-mail addresses; and the session's own
// identifiers, which mean nothing to a reader.
var leakRe = regexp.MustCompile(`(/Users/|/home/|/private/|/var/folders|/tmp/|toolu_|session_id|[\w.+-]+@[\w-]+\.[\w.-]+)`)

// Scrub fails when any text in the timeline matches leakRe. It does not
// redact: a timeline that needs redacting has kept something it should not
// have, and the fix belongs in whatever kept it.
func Scrub(tl *Timeline) error {
	return scrubValue(reflect.ValueOf(tl).Elem(), "timeline")
}

func scrubValue(v reflect.Value, where string) error {
	switch v.Kind() {
	case reflect.String:
		if m := leakRe.FindString(v.String()); m != "" {
			return fmt.Errorf("%s would publish %q: %q", where, m, v.String())
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := scrubValue(v.Index(i), fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			if err := scrubValue(v.Field(i), where+"."+v.Type().Field(i).Name); err != nil {
				return err
			}
		}
	}
	return nil
}
