package supervisor

import (
	"fmt"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

const dedupCacheSizePerAgent = 1000

type eventDedupCache struct {
	cacheSize int
	mu        sync.Mutex
	caches    map[string]*lru.Cache[string, struct{}]
}

func newEventDedupCache(cacheSize int) (*eventDedupCache, error) {
	if cacheSize <= 0 {
		return nil, fmt.Errorf("cache size must be positive")
	}

	return &eventDedupCache{
		cacheSize: cacheSize,
		caches:    make(map[string]*lru.Cache[string, struct{}]),
	}, nil
}

func (d *eventDedupCache) seen(eventKey string) bool {
	agentID, eventID, ok := strings.Cut(eventKey, ":")
	if !ok {
		return false
	}
	if agentID == "" || eventID == "" {
		return false
	}

	d.mu.Lock()
	cache, exists := d.caches[agentID]
	if !exists {
		var err error
		cache, err = lru.New[string, struct{}](d.cacheSize)
		if err != nil {
			d.mu.Unlock()
			return false
		}
		d.caches[agentID] = cache
	}

	if cache.Contains(eventID) {
		d.mu.Unlock()
		return true
	}

	cache.Add(eventID, struct{}{})
	d.mu.Unlock()
	return false
}
