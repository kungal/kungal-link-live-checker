package checker

import "net/url"

// Registry holds the ordered set of provider checkers.
type Registry struct {
	checkers []Checker
}

// NewRegistry builds a registry from the given checkers, in priority order.
func NewRegistry(cs ...Checker) *Registry {
	return &Registry{checkers: append([]Checker(nil), cs...)}
}

// Register appends a checker at the lowest priority.
func (r *Registry) Register(c Checker) { r.checkers = append(r.checkers, c) }

// Match returns the first checker whose Matches reports true, or nil when no
// provider handles the URL (the caller maps that to unsupported_provider).
func (r *Registry) Match(u *url.URL) Checker {
	for _, c := range r.checkers {
		if c.Matches(u) {
			return c
		}
	}
	return nil
}

// Names returns the registered provider names, in order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.checkers))
	for i, c := range r.checkers {
		names[i] = c.Name()
	}
	return names
}
