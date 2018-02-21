package calc

import "sort"

type setEntry struct{}

type symbolSet map[string]setEntry

func newSymbolSet() symbolSet {
	return map[string]setEntry{}
}

func (s symbolSet) Add(name string) {
	s[name] = setEntry{}
}

func (s symbolSet) AddAll(other symbolSet) {
	for name := range other {
		s.Add(name)
	}
}

func (s symbolSet) Remove(name string) {
	if s == nil {
		return
	}
	delete(s, name)
}

func (s symbolSet) Has(name string) bool {
	if s == nil {
		return false
	}
	_, has := s[name]
	return has
}

func (s symbolSet) Empty() bool {
	return len(s) == 0
}

func (s symbolSet) AppendNames(names []string) []string {
	start := len(names)
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names[start:])
	return names
}

// TakeAnyOne will remove a single arbitrary item from the set and return it.
// If the set is empty, the result is always the empty string.
func (s symbolSet) TakeAnyOne() string {
	for k := range s {
		s.Remove(k)
		return k
	}
	return ""
}
