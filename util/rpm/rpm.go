package rpm

import (
	"regexp"
	"strings"
)

// referred fields's key
var keys = []string{"Version", "Release"}

// Spec spec information
type Spec struct {
	// marcos define in spec
	macros map[string]string
	// referred fields
	values map[string]string
	lines  []string
}

// NewSpec new a spec instance include information about a spec file
func NewSpec(content string) *Spec {
	s := &Spec{
		macros: make(map[string]string),
		values: make(map[string]string),
	}
	s.lines = strings.Split(content, "\n")
	s.parse()
	return s
}

// identify identify the macros define and load into map
func (s *Spec) identify(line string) {
	re := regexp.MustCompile(`^\s*(?:%define|%global)\s+(?P<key>\w+)\s+(?P<value>[^\s]+)`)
	match := re.FindStringSubmatch(line)
	if match != nil {
		s.macros[match[1]] = match[2]
	}
}

// expand iter spec lines and expand macros
func (s *Spec) expand() {
	for i := range s.lines {
		for k, v := range s.macros {
			re := regexp.MustCompile(`%{\??` + k + `}`)
			s.lines[i] = re.ReplaceAllString(s.lines[i], v)
		}
		s.identify(s.lines[i])
	}
}

// extract
func (s *Spec) extract() {
	for _, key := range keys {
		re := regexp.MustCompile(`^\s*` + key + `\s*:\s+([^\s]+)`)
		for _, line := range s.lines {
			match := re.FindStringSubmatch(line)
			if match != nil {
				s.values[key] = match[1]
			}
		}
	}
}

func (s *Spec) parse() {
	s.expand()
	s.extract()
}

// Version get Version from spec
func (s *Spec) Version() string {
	return s.values["Version"]
}

// Release get Release from spec
func (s *Spec) Release() string {
	return s.values["Release"]
}
