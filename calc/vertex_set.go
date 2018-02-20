package calc

type edgeSet map[string]symbolSet

func newEdgeSet() edgeSet {
	return map[string]symbolSet{}
}

func (s edgeSet) Add(from, to string) {
	if _, exists := s[from]; !exists {
		s[from] = newSymbolSet()
	}
	s[from].Add(to)
}

func (s edgeSet) Remove(from, to string) {
	s[from].Remove(to)
	if s[from].Empty() {
		delete(s, from)
	}
}

func (s edgeSet) Has(from, to string) bool {
	return s[from].Has(to)
}

func (s edgeSet) AllFrom(from string) symbolSet {
	return s[from]
}

func (s edgeSet) FromHasAny(from string) bool {
	return !s[from].Empty()
}

func (s edgeSet) RemoveFrom(from string) {
	delete(s, from)
}

func (s edgeSet) AddAll(other edgeSet) {
	for from, tos := range other {
		for to := range tos {
			s.Add(from, to)
		}
	}
}
