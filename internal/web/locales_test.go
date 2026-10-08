package web

import (
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestLocalesAligned guards ENF-05: every catalogue has the same keys.
func TestLocalesAligned(t *testing.T) {
	keys := func(file string) map[string]bool {
		b, err := os.ReadFile("../../locales/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var tree map[string]any
		if err := yaml.Unmarshal(b, &tree); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		var walk func(prefix string, m map[string]any)
		walk = func(prefix string, m map[string]any) {
			for k, v := range m {
				if sub, ok := v.(map[string]any); ok {
					walk(prefix+k+".", sub)
					continue
				}
				out[prefix+k] = true
			}
		}
		walk("", tree)
		return out
	}
	fr, en := keys("fr.yaml"), keys("en.yaml")
	var missing []string
	for k := range fr {
		if !en[k] {
			missing = append(missing, "en: "+k)
		}
	}
	for k := range en {
		if !fr[k] {
			missing = append(missing, "fr: "+k)
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Error("missing key", m)
	}
}
