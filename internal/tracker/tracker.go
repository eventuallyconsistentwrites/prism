// holds the global memory maps
// eg: map[string]*hll.HLL
// /   map[string]*hashet.
// Tracker — a map of counting machines, with locks
package tracker

import (
	"log"
	"sync"

	"github.com/eventuallyconsistentwrites/prism/internal/hashset"
	"github.com/eventuallyconsistentwrites/prism/internal/hll"
)

const DefaultErrorRate = 0.01

type Tracker struct {
	mu           sync.RWMutex //to prevent multiple go routines from accessing the map tracke
	entries      map[string]*entry
	errorRate    float64
	benchMarking bool
}

// having seprate entry struct much cleaner, allows simultaneous updates to both hll and hashset, and
// allows for per entry locking, else we would be making a map
// else we will have to have an additonal lock 'locks map[string]*sync.Mutex' for per entry locking in tracker
type entry struct {
	mu      sync.RWMutex //per entry locking, do not lock the entire tracker to update a single entry in it, other can be updated parallely within it
	hll     *hll.HyperLogLog
	hashSet *hashset.HashSet //for benchmarking
}

func NewTracker(errorRate float64, benchMarking bool) *Tracker {
	if errorRate <= 0 {
		errorRate = DefaultErrorRate
	}
	return &Tracker{
		entries:      make(map[string]*entry),
		errorRate:    errorRate,
		benchMarking: benchMarking,
	}
}

// Tracker is called when a request to a path is made, to add it to our Hll and if becnhmarking, hashset too
func (t *Tracker) Track(key, ipaddr string) {
	//read-lock and look up the key
	//each entry has its own hyperloglog, as well as own hashset during benchmarking mode to compare
	t.mu.RLock()
	e, exists := t.entries[key]
	t.mu.RUnlock()
	//if the key, doesn't exists, acquire a write lock, and make an entry for that key(path)
	//making an entry involves creating new map(s) for the incoming path
	//eg if 'page-a' has its first visitor, a new entry would be created for which 'page-a' has its own HyperLogLog
	//similarily for 'page-b', on first visitr, a new hll/hashset would be created for that path
	if !exists {
		t.mu.Lock() //write lock the map

		//double check if it was already written by some other go routine while acquiring the lock
		if e, exists = t.entries[key]; !exists {
			//create an entry
			e = &entry{
				hll: hll.NewHyperLogLog(t.errorRate),
			}
			if t.benchMarking {
				e.hashSet = hashset.NewHashSet()
			}
			t.entries[key] = e
		}
		t.mu.Unlock()
	}

	//now the entry definitely exists, we can track the ip
	//seperate lock for entry, instead of the locking entire tracker
	//decouples locking tracker vs locking a specific entry inside it, leading to improved performance
	e.mu.Lock()
	e.hll.Add(hll.GetHashString(ipaddr))
	if t.benchMarking {
		if e.hashSet == nil {
			e.hashSet = hashset.NewHashSet()
		}
		e.hashSet.Add(ipaddr)
	}
	e.mu.Unlock()
}

// called for analytics, tells the number of unique visitors on a page with standard error rate
func (t *Tracker) Estimate(key string) (uint64, bool) {
	//check if the key even exists
	t.mu.RLock()
	e, exists := t.entries[key]
	t.mu.RUnlock()
	if !exists {
		return uint64(0), false //path not visited, false lets it repsond with 404
	}
	e.mu.RLock()
	val := e.hll.Estimate()
	if t.benchMarking {
		exactVal := e.hashSet.Count()
		log.Printf("Exact val is: %d", exactVal)
	}
	e.mu.RUnlock()
	return uint64(val), true

}
