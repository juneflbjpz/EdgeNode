package caches

import (
	"github.com/TeaOSLab/EdgeCommon/pkg/configutils"
	"github.com/TeaOSLab/EdgeNode/internal/zero"
	"github.com/iwind/TeaGo/logs"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// MemoryList 内存缓存列表管理
type MemoryList struct {
	count int64

	itemMaps map[string]map[string]*Item // prefix => { hash => item }

	weekItemMaps map[int32]map[string]zero.Zero // week => { hash => Zero }
	minWeek      int32

	prefixes []string
	locker   sync.RWMutex
	onAdd    func(item *Item)
	onRemove func(item *Item)

	purgeIndex int
}

func NewMemoryList() ListInterface {
	return &MemoryList{
		itemMaps:     map[string]map[string]*Item{},
		weekItemMaps: map[int32]map[string]zero.Zero{},
		minWeek:      currentWeek(),
	}
}

func (this *MemoryList) Init() error {
	this.prefixes = []string{"000"}
	for i := 100; i <= 999; i++ {
		this.prefixes = append(this.prefixes, strconv.Itoa(i))
	}

	for _, prefix := range this.prefixes {
		this.itemMaps[prefix] = map[string]*Item{}
	}

	return nil
}

func (this *MemoryList) Reset() error {
	this.locker.Lock()
	for key := range this.itemMaps {
		this.itemMaps[key] = map[string]*Item{}
	}
	this.weekItemMaps = map[int32]map[string]zero.Zero{}
	this.locker.Unlock()

	atomic.StoreInt64(&this.count, 0)

	return nil
}

func (this *MemoryList) Add(hash string, item *Item) error {
	if item.Week == 0 {
		item.Week = currentWeek()
	}

	this.locker.Lock()

	prefix := this.prefix(hash)
	itemMap, ok := this.itemMaps[prefix]
	if !ok {
		itemMap = map[string]*Item{}
		this.itemMaps[prefix] = itemMap
	}

	// 先删除，为了可以正确触发统计
	oldItem, ok := itemMap[hash]
	if ok {
		// 从week map中删除
		if oldItem.Week > 0 {
			wm, ok := this.weekItemMaps[oldItem.Week]
			if ok {
				delete(wm, hash)
			}
		}

		// 回调
		if this.onRemove != nil {
			this.onRemove(oldItem)
		}
	} else {
		atomic.AddInt64(&this.count, 1)
	}

	// 添加
	if this.onAdd != nil {
		this.onAdd(item)
	}

	itemMap[hash] = item

	// week map
	wm, ok := this.weekItemMaps[item.Week]
	if ok {
		wm[hash] = zero.New()
	} else {
		this.weekItemMaps[item.Week] = map[string]zero.Zero{hash: zero.New()}
	}

	this.locker.Unlock()
	return nil
}

func (this *MemoryList) Exist(hash string) (bool, error) {
	this.locker.RLock()
	defer this.locker.RUnlock()

	prefix := this.prefix(hash)
	itemMap, ok := this.itemMaps[prefix]
	if !ok {
		return false, nil
	}
	item, ok := itemMap[hash]
	if !ok {
		return false, nil
	}

	return !item.IsExpired(), nil
}

// CleanPrefix 根据前缀进行清除
func (this *MemoryList) CleanPrefix(prefix string) error {
	this.locker.RLock()
	defer this.locker.RUnlock()

	// TODO 需要优化性能，支持千万级数据低于1s的处理速度
	for _, itemMap := range this.itemMaps {
		for _, item := range itemMap {
			if strings.HasPrefix(item.Key, prefix) {
				item.ExpiredAt = 0
			}
		}
	}
	return nil
}

// CleanMatchKey 清理通配符匹配的缓存数据，类似于 https://*.example.com/hello
func (this *MemoryList) CleanMatchKey(key string) error {
	if strings.Contains(key, SuffixAll) {
		return nil
	}

	u, err := url.Parse(key)
	if err != nil {
		return nil
	}

	var host = u.Host
	hostPart, _, err := net.SplitHostPort(host)
	if err == nil && len(hostPart) > 0 {
		host = hostPart
	}

	if len(host) == 0 {
		return nil
	}
	var requestURI = u.RequestURI()

	this.locker.RLock()
	defer this.locker.RUnlock()

	// TODO 需要优化性能，支持千万级数据低于1s的处理速度
	for _, itemMap := range this.itemMaps {
		for _, item := range itemMap {
			if configutils.MatchDomain(host, item.Host) {
				var itemRequestURI = item.RequestURI()
				if itemRequestURI == requestURI || strings.HasPrefix(itemRequestURI, requestURI+SuffixAll) {
					item.ExpiredAt = 0
				}
			}
		}
	}

	return nil
}

// CleanMatchPrefix 清理通配符匹配的缓存数据，类似于 https://*.example.com/prefix/
func (this *MemoryList) CleanMatchPrefix(prefix string) error {
	u, err := url.Parse(prefix)
	if err != nil {
		return nil
	}

	var host = u.Host
	hostPart, _, err := net.SplitHostPort(host)
	if err == nil && len(hostPart) > 0 {
		host = hostPart
	}
	if len(host) == 0 {
		return nil
	}
	var requestURI = u.RequestURI()
	var isRootPath = requestURI == "/"

	this.locker.RLock()
	defer this.locker.RUnlock()

	// TODO 需要优化性能，支持千万级数据低于1s的处理速度
	for _, itemMap := range this.itemMaps {
		for _, item := range itemMap {
			if configutils.MatchDomain(host, item.Host) {
				var itemRequestURI = item.RequestURI()
				if isRootPath || strings.HasPrefix(itemRequestURI, requestURI) {
					item.ExpiredAt = 0
				}
			}
		}
	}

	return nil
}

func (this *MemoryList) Remove(hash string) error {
	this.locker.Lock()

	itemMap, ok := this.itemMaps[this.prefix(hash)]
	if !ok {
		this.locker.Unlock()
		return nil
	}

	item, ok := itemMap[hash]
	if ok {
		if this.onRemove != nil {
			this.onRemove(item)
		}

		atomic.AddInt64(&this.count, -1)
		delete(itemMap, hash)

		// week map
		if item.Week > 0 {
			wm, ok := this.weekItemMaps[item.Week]
			if ok {
				delete(wm, hash)
			}
		}
	}

	this.locker.Unlock()
	return nil
}

// Purge 清理过期的缓存
// count 每次遍历的最大数量，控制此数字可以保证每次清理的时候不用花太多时间
// callback 每次发现过期key的调用
func (this *MemoryList) Purge(count int, callback func(hash string) error) (int, error) {
	this.locker.Lock()
	deletedHashList := []string{}

	if this.purgeIndex >= len(this.prefixes) {
		this.purgeIndex = 0
	}
	prefix := this.prefixes[this.purgeIndex]

	this.purgeIndex++

	itemMap, ok := this.itemMaps[prefix]
	if !ok {
		this.locker.Unlock()
		return 0, nil
	}
	var countFound = 0
	for hash, item := range itemMap {
		if count <= 0 {
			break
		}

		if item.IsExpired() {
			if this.onRemove != nil {
				this.onRemove(item)
			}

			atomic.AddInt64(&this.count, -1)
			delete(itemMap, hash)
			deletedHashList = append(deletedHashList, hash)

			// week map
			if item.Week > 0 {
				wm, ok := this.weekItemMaps[item.Week]
				if ok {
					delete(wm, hash)
				}
			}

			countFound++
		}

		count--
	}
	this.locker.Unlock()

	// 执行外部操作
	for _, hash := range deletedHashList {
		if callback != nil {
			err := callback(hash)
			if err != nil {
				return 0, err
			}
		}
	}
	return countFound, nil
}

func (this *MemoryList) PurgeLFU(count int, callback func(hash string) error) error {
	if count <= 0 {
		return nil
	}

	var week = currentWeek()
	if this.minWeek > week {
		this.minWeek = week
	}

	var deletedHashList = []string{}

Loop:
	for w := this.minWeek; w <= week; w++ {
		this.minWeek = w

		this.locker.Lock()
		wm, ok := this.weekItemMaps[w]
		if ok {
			var wc = len(wm)
			if wc == 0 {
				delete(this.weekItemMaps, w)
			} else {
				if wc <= count {
					delete(this.weekItemMaps, w)
				}

				// TODO 未来支持按照点击量排序
				for hash := range wm {
					count--

					if count < 0 {
						this.locker.Unlock()
						break Loop
					}

					delete(wm, hash)

					itemMap, ok := this.itemMaps[this.prefix(hash)]
					if !ok {
						continue
					}
					item, ok := itemMap[hash]
					if !ok {
						continue
					}

					if this.onRemove != nil {
						this.onRemove(item)
					}

					atomic.AddInt64(&this.count, -1)
					delete(itemMap, hash)
					deletedHashList = append(deletedHashList, hash)
				}
			}
		} else {
			delete(this.weekItemMaps, w)
		}
		this.locker.Unlock()
	}

	// 执行外部操作
	for _, hash := range deletedHashList {
		if callback != nil {
			err := callback(hash)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (this *MemoryList) CleanAll() error {
	return this.Reset()
}

func (this *MemoryList) Stat(check func(hash string) bool) (*Stat, error) {
	this.locker.RLock()
	defer this.locker.RUnlock()

	result := &Stat{
		Count: 0,
		Size:  0,
	}
	for _, itemMap := range this.itemMaps {
		for hash, item := range itemMap {
			if !item.IsExpired() {
				// 检查文件是否存在、内容是否正确等
				if check != nil && check(hash) {
					result.Count++
					result.ValueSize += item.Size()
					result.Size += item.TotalSize()
				}
			}
		}
	}
	return result, nil
}

// Count 总数量
func (this *MemoryList) Count() (int64, error) {
	var count = atomic.LoadInt64(&this.count)
	return count, nil
}

// OnAdd 添加事件
func (this *MemoryList) OnAdd(f func(item *Item)) {
	this.onAdd = f
}

// OnRemove 删除事件
func (this *MemoryList) OnRemove(f func(item *Item)) {
	this.onRemove = f
}

func (this *MemoryList) Close() error {
	return nil
}

// IncreaseHit 增加点击量
func (this *MemoryList) IncreaseHit(hash string) error {
	this.locker.Lock()

	itemMap, ok := this.itemMaps[this.prefix(hash)]
	if !ok {
		this.locker.Unlock()
		return nil
	}

	item, ok := itemMap[hash]
	if ok {
		var week = currentWeek()

		// 交换位置
		if item.Week > 0 && item.Week != week {
			wm, ok := this.weekItemMaps[item.Week]
			if ok {
				delete(wm, hash)
			}
			wm, ok = this.weekItemMaps[week]
			if ok {
				wm[hash] = zero.New()
			} else {
				this.weekItemMaps[week] = map[string]zero.Zero{hash: zero.New()}
			}
		}

		item.IncreaseHit(week)
	}

	this.locker.Unlock()
	return nil
}

func (this *MemoryList) print(t *testing.T) {
	this.locker.Lock()
	for _, itemMap := range this.itemMaps {
		if len(itemMap) > 0 {
			logs.PrintAsJSON(itemMap, t)
		}
	}
	this.locker.Unlock()
}

func (this *MemoryList) prefix(hash string) string {
	var prefix string
	if len(hash) > 3 {
		prefix = hash[:3]
	} else {
		prefix = hash
	}
	_, ok := this.itemMaps[prefix]
	if !ok {
		prefix = "000"
	}
	return prefix
}
