package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// LRU is a fixed-capacity, in-process cache with per-entry expiry.
//
// It exists to absorb the hits that would otherwise cross the network to Redis.
// On a free tier that round trip is tens of milliseconds, which is most of what
// caching was supposed to save, so the first tier has to be local.
type LRU struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element
	order    *list.List

	// now is injectable so expiry can be tested without sleeping.
	now func() time.Time
}

type entry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// NewLRU returns a cache holding at most capacity entries. A capacity below one
// is raised to one: a cache that stores nothing is a bug, not a configuration.
func NewLRU(capacity int) *LRU {
	if capacity < 1 {
		capacity = 1
	}
	return &LRU{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
		now:      time.Now,
	}
}

func (c *LRU) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return nil, ErrMiss
	}

	item := element.Value.(*entry)
	if c.now().After(item.expiresAt) {
		c.removeElement(element)
		return nil, ErrMiss
	}

	c.order.MoveToFront(element)

	// A copy, not the stored slice: a caller that modified the result would
	// silently corrupt what every later hit returns.
	value := make([]byte, len(item.value))
	copy(value, item.value)
	return value, nil
}

func (c *LRU) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	stored := make([]byte, len(value))
	copy(stored, value)
	expiresAt := c.now().Add(ttl)

	if element, ok := c.items[key]; ok {
		item := element.Value.(*entry)
		item.value = stored
		item.expiresAt = expiresAt
		c.order.MoveToFront(element)
		return nil
	}

	c.items[key] = c.order.PushFront(&entry{key: key, value: stored, expiresAt: expiresAt})

	if c.order.Len() > c.capacity {
		if oldest := c.order.Back(); oldest != nil {
			c.removeElement(oldest)
		}
	}
	return nil
}

// Len reports how many entries are held, expired ones included: they are only
// dropped when looked up or evicted.
func (c *LRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *LRU) removeElement(element *list.Element) {
	c.order.Remove(element)
	delete(c.items, element.Value.(*entry).key)
}
