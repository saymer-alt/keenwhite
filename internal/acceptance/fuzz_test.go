package acceptance

import (
	"testing"
)

// FuzzDecodeRecord protects the strict record decoder (task 14 stage E):
// any byte sequence either fails decoding or yields a structurally sane
// record; validation afterwards must judge it without panicking.
func FuzzDecodeRecord(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"record_version":1,"result":"PASS"}`))
	f.Add([]byte(`{"record_version":1} trailing`))
	f.Add([]byte(`{"record_version":1,"metrics":{"peak_rss_kb":26900}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := decodeRecord(data)
		if err != nil {
			return
		}
		// A record from a future schema must be flagged by validation
		// (decode alone is never acceptance).
		if r.RecordVersion != RecordVersion {
			vf := ValidateRecord(r, now)
			flagged := false
			for _, x := range vf {
				if x.Code == "RECORD_VERSION" {
					flagged = true
				}
			}
			if !flagged {
				t.Fatal("non-v1 record_version must be flagged by validation")
			}
		}
		_ = ValidateRecord(r, now) // must never panic on decoder output
	})
}
