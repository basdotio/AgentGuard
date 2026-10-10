// SPDX-License-Identifier: MIT

package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Annotation is the part of a sample's label (`<path>.yaml`, beside the sample tree) this rig
// reads: enough to say which source the sample belongs to, and nothing about what it is. The
// label's truth stays the corpus's business.
type Annotation struct {
	ID      string   `yaml:"id"`
	Class   string   `yaml:"class"`
	Surface surfaces `yaml:"surface"`
	Origin  struct {
		Type        string `yaml:"type"`
		Source      string `yaml:"source"`
		DerivedFrom *struct {
			Entry string `yaml:"entry"`
		} `yaml:"derived_from"`
	} `yaml:"origin"`
}

// surfaces is a label's surface: one string or a list, the first being primary.
type surfaces []string

func (s *surfaces) UnmarshalYAML(value *yaml.Node) error {
	var one string
	if err := value.Decode(&one); err == nil {
		*s = surfaces{one}
		return nil
	}
	var many []string
	if err := value.Decode(&many); err != nil {
		return fmt.Errorf("surface must be a string or a list of strings: %w", err)
	}
	*s = many
	return nil
}

// ReadAnnotation reads the label of one work-list sample. The label must be that sample's: a
// work list pointing at another sample's file would count it under someone else's source.
func ReadAnnotation(corpusDir string, s Sample) (Annotation, error) {
	p := filepath.Join(corpusDir, filepath.FromSlash(s.Path)) + ".yaml"
	b, err := os.ReadFile(p)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation of %s: %w", s.Sample, err)
	}
	var a Annotation
	if err := yaml.Unmarshal(b, &a); err != nil {
		return Annotation{}, fmt.Errorf("annotation of %s does not parse: %w", s.Sample, err)
	}
	if a.ID != s.Sample {
		return Annotation{}, fmt.Errorf("annotation %s.yaml names sample %q, the work list %q", s.Path, a.ID, s.Sample)
	}
	return a, nil
}

// originRepo is the corpus's: the ORIGINATING repository named in a source, not the collection
// that redistributed it ("https://github.com/o/r (via skillmd-138k @ abc)" is r's).
var originRepo = regexp.MustCompile(`github\.com/([^/\s]+/[^/\s)]+)`)

// Source is the batch the sample came from, by the corpus's own rule — populationOf in its
// harness/cmd/corpus/basis.go, which `corpus samples --source` filters on and the scorecard groups
// by. It is a copy kept in step by hand, the way DimensionMap is, because the corpus is another
// module this one does not import; TestSource_AgreesWithTheCorpus compares the two when a
// checkout is at hand. A different definition here would give rows no scorecard can be set beside.
func (a Annotation) Source() string {
	switch {
	case a.Origin.DerivedFrom != nil && a.Origin.DerivedFrom.Entry != "":
		return a.Origin.DerivedFrom.Entry
	case a.Origin.Type == "harvested":
		// A harvested label of any class but benign is one a person promoted by hand.
		if a.Class != "benign" {
			return "promoted-" + a.Class
		}
		return "harvested"
	case a.Origin.Type == "reconstruction":
		primary := ""
		if len(a.Surface) > 0 {
			primary = a.Surface[0]
		}
		return "reconstruction:" + primary
	case a.Origin.Type == "synthetic":
		return "synthetic-" + a.Class
	}
	if m := originRepo.FindStringSubmatch(a.Origin.Source); m != nil {
		return m[1]
	}
	return "hand-written"
}
