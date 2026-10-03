package engine

import "sync"

// cacheEntry 是一条编译缓存项。
type cacheEntry struct {
	key  string
	comp *CompiledTemplate
}

// compileCache 是内容寻址的编译结果缓存。
// 缓存项一经写入不可变；源模板变化会产生新 key，
// 旧项自然失效，因此并发渲染永远不会读到半成品。
type compileCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // key -> 编译产物
	byName  map[string]string       // 模板名 -> key
}

func newCompileCache() *compileCache {
	return &compileCache{
		entries: make(map[string]*cacheEntry),
		byName:  make(map[string]string),
	}
}

func (c *compileCache) get(key string) (*CompiledTemplate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	return e.comp, true
}

func (c *compileCache) put(name, key string, comp *CompiledTemplate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{key: key, comp: comp}
	c.byName[name] = key
}

// currentKey 返回模板当前源码对应的缓存 key。
func (c *compileCache) currentKey(name string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, ok := c.byName[name]
	return k, ok
}
