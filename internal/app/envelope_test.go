package app

import "testing"

func TestLosslessEnvelopeNormalizationOnly(t *testing.T) {
	allowed := map[string]bool{"a.cpp": true}
	for _, tc := range []struct{ input, normal string }{
		{`{"files":{"a.cpp":"int main(){}\n"}}`, "none"},
		{`{"a.cpp":"int main(){}\n"}`, "direct_file_map"},
		{`{"files":{"a.cpp":"int main(){}\n"}`, "closed_outer_object"},
	} {
		files, normal, e := parseFilesEnvelope(tc.input, allowed)
		if e != nil || normal != tc.normal || string(files["a.cpp"]) != "int main(){}\n" {
			t.Fatalf("changed source bytes or classification: %q %q %v", files, normal, e)
		}
	}
	for _, input := range []string{
		`{"files":{"a.cpp":"unfinished`,
		`{"files":{"a.cpp":"x"`,
		`{"files":{"a.cpp":"x"},"execute":"bad"}`,
		`{"files":{"../escape":"x"}`, `{"unknown":"x"}`, `{"files":{}}`,
		`{"files":{"a.cpp":null}}`,
	} {
		if _, _, e := parseFilesEnvelope(input, allowed); e == nil {
			t.Fatalf("accepted invalid envelope %s", input)
		}
	}
}
