// naive implementation
package hashset

type HashSet struct {
	items map[string]struct{}
}

func NewHashSet() *HashSet {
	return &HashSet{
		items: make(map[string]struct{}),
	}
}

func (h *HashSet) Add(item string) {
	h.items[item] = struct{}{}
}

func (h *HashSet) Count() uint64 {
	return uint64(len(h.items))
}
