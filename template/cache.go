package template

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// cacheEntry 缓存条目：内容指纹 + 编译产物。
// 条目一旦写入不再原地修改，源变化时整体替换，读者永远拿到完整产物。
type cacheEntry struct {
	fingerprint string
	tpl         *Template
}

// Cache 提供并发安全的编译缓存。
// 缓存键由模板名与源内容指纹共同决定：源变化后自动重新编译。
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	// inflight 按名字合并并发编译，避免惊群。
	inflight map[string]*compileCall
}

type compileCall struct {
	done chan struct{}
	tpl  *Template
	err  error
}

// NewCache 创建缓存。
func NewCache() *Cache {
	return &Cache{
		entries:  map[string]*cacheEntry{},
		inflight: map[string]*compileCall{},
	}
}

// fingerprint 计算源内容的稳定指纹。
func fingerprint(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Get 按名字获取已编译模板；未命中或源已变化时编译并原子替换缓存。
func (c *Cache) Get(name string, src Source) (*Template, error) {
	raw, ok := src.Get(name)
	if !ok {
		return nil, &MissingError{Missing: name}
	}
	fp := fingerprint(raw)
	c.mu.RLock()
	ent, hit := c.entries[name]
	c.mu.RUnlock()
	if hit && ent.fingerprint == fp {
		return ent.tpl, nil
	}
	return c.compileLocked(name, src, fp)
}

// compileLocked 合并同名并发编译，完成后原子写入缓存。
func (c *Cache) compileLocked(name string, src Source, fp string) (*Template, error) {
	c.mu.Lock()
	if call, ok := c.inflight[name]; ok {
		c.mu.Unlock()
		<-call.done
		return call.tpl, call.err
	}
	call := &compileCall{done: make(chan struct{})}
	c.inflight[name] = call
	c.mu.Unlock()

	tpl, err := Compile(name, src)

	c.mu.Lock()
	delete(c.inflight, name)
	if err == nil {
		c.entries[name] = &cacheEntry{fingerprint: fp, tpl: tpl}
	}
	call.tpl, call.err = tpl, err
	close(call.done)
	c.mu.Unlock()
	return tpl, err
}

// Load 优先使用序列化产物；产物不兼容或损坏时安全回退到重新编译。
// 回退结果同样写入缓存，两条路径的产物语义完全一致。
func (c *Cache) Load(name string, src Source, blob []byte) (*Template, error) {
	if len(blob) > 0 {
		if tpl, err := Unmarshal(blob); err == nil && tpl.Name() == name {
			c.mu.Lock()
			raw, _ := src.Get(name)
			c.entries[name] = &cacheEntry{fingerprint: fingerprint(raw), tpl: tpl}
			c.mu.Unlock()
			return tpl, nil
		}
		// Unmarshal 失败（*VersionError：版本不兼容或数据损坏），安全回退到重新编译
	}
	return c.Get(name, src)
}

// Invalidate 使指定模板缓存失效，下次 Get 将重新编译。
func (c *Cache) Invalidate(name string) {
	c.mu.Lock()
	delete(c.entries, name)
	c.mu.Unlock()
}
