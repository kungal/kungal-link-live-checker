package checker

import "net/url"

type Registry struct {
	checkers []Checker
}

func NewRegistry(cs ...Checker) *Registry {
	return &Registry{checkers: append([]Checker(nil), cs...)}
}

func (r *Registry) Register(c Checker) { r.checkers = append(r.checkers, c) }

func (r *Registry) Match(u *url.URL) Checker {
	for _, c := range r.checkers {
		if c.Matches(u) {
			return c
		}
	}
	return nil
}

func (r *Registry) Names() []string {
	names := make([]string, len(r.checkers))
	for i, c := range r.checkers {
		names[i] = c.Name()
	}
	return names
}
