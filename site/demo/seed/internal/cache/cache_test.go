package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestInvalidateUnderLoad(t *testing.T) {
	c := New(func(k string) ([]byte, error) { return []byte(k), nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.Get(fmt.Sprintf("user/%d/%d", i, j))
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.Invalidate("user/")
			}
		}()
	}
	wg.Wait()
	c.Invalidate("user/")
	if n := c.Len(); n != 0 {
		t.Fatalf("%d keys survived invalidation", n)
	}
}
