// Package cache is a read-through cache with prefix invalidation.
package cache

import (
	"strings"
	"sync"
)

type Cache struct {
	mu    sync.Mutex
	items map[string][]byte
	load  func(key string) ([]byte, error)
}

func New(load func(string) ([]byte, error)) *Cache {
	return &Cache{items: map[string][]byte{}, load: load}
}

func (c *Cache) Get(key string) ([]byte, error) {
	c.mu.Lock()
	v, ok := c.items[key]
	c.mu.Unlock()
	if ok {
		return v, nil
	}
	v, err := c.load(key)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.items[key] = v
	c.mu.Unlock()
	return v, nil
}

// Invalidate drops every key under prefix.
func (c *Cache) Invalidate(prefix string) {
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			delete(c.items, k)
		}
	}
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
