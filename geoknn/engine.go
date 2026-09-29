package geoknn

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

// EarthRadius 是球体地球半径（米）。
const EarthRadius = 6371008.8

// 规模上限。
const (
	maxLayers   = 50
	maxFeatures = 500000
	maxVertices = 5000000
	maxBatch    = 5000
)

// ErrNotFound 表示要删除或替换的编号不存在。
var ErrNotFound = errors.New("要素编号不存在")

// ImportError 表示批量导入失败。Index 是批内序号（从 0 开始），
// -1 表示整批级错误（如批量超限、总量超限）。
type ImportError struct {
	Index  int
	ID     string
	Reason string
}

func (e *ImportError) Error() string {
	if e.Index >= 0 {
		return fmt.Sprintf("第 %d 个要素（编号 %q）不合法：%s", e.Index, e.ID, e.Reason)
	}
	return fmt.Sprintf("批量导入失败：%s", e.Reason)
}

// Feature 是上层加载后交给模块的要素。
type Feature struct {
	ID    string // 全局唯一编号
	Layer string // 所属图层
	WKT   string // WKT 几何（WGS84 经纬度，经度在前）
}

// Query 是一次最近邻查询。
type Query struct {
	Lon, Lat    float64  // 查询点（度）
	K           int      // 返回数量，1..100
	MaxDistance float64  // 可选最大距离（米），0 表示不限；否则须在 [1, 50000]
	Layers      []string // 可选图层范围，空表示全部图层
}

// Result 是一个命中的要素。
type Result struct {
	ID       string
	Layer    string
	Distance float64 // 米
}

// capData 是要素包围球冠的预计算数据（查询热数据，连续存储）。
type capData struct {
	cx, cy, cz float64 // 球冠中心（单位向量）
	cosR, sinR float64 // cos/sin(球冠角半径)
	rad        float64 // 球冠角半径（弧度）
}

// Engine 是空间最近邻查询引擎，可并发使用。
// 查询与编辑通过读写锁互斥：每次查询看到某一时刻的完整数据，
// 一批导入要么全看到，要么全看不到。
//
// 存储按槽位组织：热数据（包围球冠）连续存放便于全量扫描，
// 删除用交换移除，单个要素的增删改都是 O(1)。
type Engine struct {
	mu          sync.RWMutex
	byID        map[string]int   // 编号 → 槽位
	layerIDs    map[string]int32 // 图层名 → 图层号
	layerNames  []string         // 图层号 → 图层名
	layerSlots  [][]int          // 图层号 → 槽位列表
	ids         []string         // 槽位 → 编号
	layerOf     []int32          // 槽位 → 图层号
	layerPos    []int32          // 槽位 → 在 layerSlots[layerOf] 中的下标
	caps        []capData        // 槽位 → 包围球冠
	geoms       []*geometry      // 槽位 → 几何
	vertexCount int
}

// NewEngine 创建空引擎。
func NewEngine() *Engine {
	return &Engine{
		byID:     make(map[string]int),
		layerIDs: make(map[string]int32),
	}
}

// forEachVertex 遍历几何的所有顶点。
func (g *geometry) forEachVertex(fn func(vec3)) {
	for _, v := range g.points {
		fn(v)
	}
	for _, l := range g.lines {
		for _, e := range l {
			fn(e.a)
		}
		if len(l) > 0 {
			fn(l[len(l)-1].b)
		}
	}
	for _, p := range g.polys {
		for _, r := range p.rings {
			for _, e := range r.edges {
				fn(e.a)
			}
		}
	}
}

// computeCap 计算几何的包围球冠（中心 + 角半径）。
func computeCap(g *geometry) (center vec3, radius float64) {
	sum := vec3{}
	var first vec3
	g.forEachVertex(func(v vec3) {
		sum = vAdd(sum, v)
		first = v
	})
	if vNorm(sum) < 1e-9 {
		center = first // 顶点关于球心近似对称时退化为任取一个顶点
	} else {
		center = vNormalize(sum)
	}
	g.forEachVertex(func(v vec3) {
		if d := angleBetween(center, v); d > radius {
			radius = d
		}
	})
	return center, radius
}

// capOf 计算几何的包围球冠热数据。
// 角半径 >= π/2 时球冠不再凸，"几何在球冠内" 的不变式不成立，
// 此时把 rad 记为 +Inf，查询时对这样的要素不做剪枝。
func capOf(g *geometry) capData {
	center, radius := computeCap(g)
	c := capData{
		cx:   center[0],
		cy:   center[1],
		cz:   center[2],
		cosR: math.Cos(radius),
		sinR: math.Sin(radius),
		rad:  radius,
	}
	if radius >= math.Pi/2 {
		c.rad = math.Inf(1)
	}
	return c
}

// parseFeature 解析并校验一个要素的 WKT。
func parseFeature(f Feature) (*geometry, error) {
	raw, err := parseWKT(f.WKT)
	if err != nil {
		return nil, fmt.Errorf("WKT 解析失败：%v", err)
	}
	geom, err := buildGeometry(raw)
	if err != nil {
		return nil, err
	}
	return geom, nil
}

// layerSlot 返回图层号（不存在则创建）。调用方需持有写锁。
func (e *Engine) layerSlot(name string) int32 {
	if id, ok := e.layerIDs[name]; ok {
		return id
	}
	id := int32(len(e.layerNames))
	e.layerIDs[name] = id
	e.layerNames = append(e.layerNames, name)
	e.layerSlots = append(e.layerSlots, nil)
	return id
}

// Import 批量导入要素（每批最多 5000 个）。
// 任一要素不合法则整批不生效，并报告是第几个要素、什么原因。
func (e *Engine) Import(features []Feature) error {
	if len(features) == 0 {
		return nil
	}
	if len(features) > maxBatch {
		return &ImportError{Index: -1, Reason: fmt.Sprintf("每批最多 %d 个要素，本批 %d 个", maxBatch, len(features))}
	}
	// 加锁前完成解析与校验，避免长时间阻塞查询。
	geoms := make([]*geometry, len(features))
	caps := make([]capData, len(features))
	seen := make(map[string]struct{}, len(features))
	totalVerts := 0
	for i, f := range features {
		if f.ID == "" {
			return &ImportError{Index: i, ID: f.ID, Reason: "编号为空"}
		}
		if _, dup := seen[f.ID]; dup {
			return &ImportError{Index: i, ID: f.ID, Reason: "编号重复"}
		}
		seen[f.ID] = struct{}{}
		geom, err := parseFeature(f)
		if err != nil {
			return &ImportError{Index: i, ID: f.ID, Reason: err.Error()}
		}
		totalVerts += geom.vertexCount()
		geoms[i] = geom
		caps[i] = capOf(geom)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for i, f := range features {
		if _, dup := e.byID[f.ID]; dup {
			return &ImportError{Index: i, ID: f.ID, Reason: "编号重复"}
		}
	}
	if len(e.ids)+len(features) > maxFeatures {
		return &ImportError{Index: -1, Reason: fmt.Sprintf("要素总数超过上限 %d", maxFeatures)}
	}
	if e.vertexCount+totalVerts > maxVertices {
		return &ImportError{Index: -1, Reason: fmt.Sprintf("顶点总数超过上限 %d", maxVertices)}
	}
	newLayerSet := make(map[string]struct{})
	for _, f := range features {
		if _, ok := e.layerIDs[f.Layer]; !ok {
			newLayerSet[f.Layer] = struct{}{}
		}
	}
	if len(e.layerNames)+len(newLayerSet) > maxLayers {
		return &ImportError{Index: -1, Reason: fmt.Sprintf("图层数超过上限 %d", maxLayers)}
	}
	for i, f := range features {
		slot := len(e.ids)
		lid := e.layerSlot(f.Layer)
		e.byID[f.ID] = slot
		e.ids = append(e.ids, f.ID)
		e.layerOf = append(e.layerOf, lid)
		e.layerPos = append(e.layerPos, int32(len(e.layerSlots[lid])))
		e.layerSlots[lid] = append(e.layerSlots[lid], slot)
		e.caps = append(e.caps, caps[i])
		e.geoms = append(e.geoms, geoms[i])
	}
	e.vertexCount += totalVerts
	return nil
}

// removeSlot 交换移除槽位。调用方需持有写锁。
func (e *Engine) removeSlot(slot int) {
	last := len(e.ids) - 1
	// 从图层槽位列表移除
	lid := e.layerOf[slot]
	slots := e.layerSlots[lid]
	pos := e.layerPos[slot]
	slots[pos] = slots[len(slots)-1]
	e.layerPos[slots[pos]] = pos
	slots = slots[:len(slots)-1]
	e.layerSlots[lid] = slots
	if len(slots) == 0 {
		// 图层变空：交换移除图层号，保持图层数为当前非空图层数
		lastLid := int32(len(e.layerNames) - 1)
		if lid != lastLid {
			e.layerNames[lid] = e.layerNames[lastLid]
			e.layerSlots[lid] = e.layerSlots[lastLid]
			e.layerIDs[e.layerNames[lid]] = lid
			for _, s := range e.layerSlots[lid] {
				e.layerOf[s] = lid
			}
		}
		e.layerNames = e.layerNames[:lastLid]
		e.layerSlots = e.layerSlots[:lastLid]
		delete(e.layerIDs, e.layerNames[lastLid : lastLid+1][0])
	}
	// 交换移除槽位本体
	if slot != last {
		e.ids[slot] = e.ids[last]
		e.layerOf[slot] = e.layerOf[last]
		e.layerPos[slot] = e.layerPos[last]
		e.caps[slot] = e.caps[last]
		e.geoms[slot] = e.geoms[last]
		e.byID[e.ids[slot]] = slot
		e.layerSlots[e.layerOf[slot]][e.layerPos[slot]] = slot
	}
	e.ids = e.ids[:last]
	e.layerOf = e.layerOf[:last]
	e.layerPos = e.layerPos[:last]
	e.caps = e.caps[:last]
	e.geoms = e.geoms[:last]
}

// Delete 删除要素；编号不存在时报错。
func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	slot, ok := e.byID[id]
	if !ok {
		return ErrNotFound
	}
	e.vertexCount -= e.geoms[slot].vertexCount()
	delete(e.byID, id)
	e.removeSlot(slot)
	return nil
}

// ReplaceGeometry 替换要素几何（校验规则与导入相同）；编号不存在时报错。
func (e *Engine) ReplaceGeometry(id, wkt string) error {
	geom, err := parseFeature(Feature{ID: id, WKT: wkt})
	if err != nil {
		return err
	}
	cap := capOf(geom)
	e.mu.Lock()
	defer e.mu.Unlock()
	slot, ok := e.byID[id]
	if !ok {
		return ErrNotFound
	}
	oldVerts := e.geoms[slot].vertexCount()
	if e.vertexCount-oldVerts+geom.vertexCount() > maxVertices {
		return fmt.Errorf("顶点总数超过上限 %d", maxVertices)
	}
	e.geoms[slot] = geom
	e.caps[slot] = cap
	e.vertexCount += geom.vertexCount() - oldVerts
	return nil
}

// cand 是查询候选。
type cand struct {
	slot int
	dist float64 // 弧度
}

// candHeap 是最大堆，堆顶为当前最差候选（距离更远，或距离相同编号更大）。
type candHeap struct {
	c   []cand
	ids []string // 引擎的 ids 切片，用于比较编号
}

func (h *candHeap) Len() int { return len(h.c) }
func (h *candHeap) worse(x, y cand) bool {
	if x.dist != y.dist {
		return x.dist > y.dist
	}
	return h.ids[x.slot] > h.ids[y.slot]
}
func (h *candHeap) Less(i, j int) bool { return h.worse(h.c[i], h.c[j]) }
func (h *candHeap) Swap(i, j int)      { h.c[i], h.c[j] = h.c[j], h.c[i] }
func (h *candHeap) Push(x any)         { h.c = append(h.c, x.(cand)) }
func (h *candHeap) Pop() any {
	old := h.c
	n := len(old)
	top := old[n-1]
	h.c = old[:n-1]
	return top
}

// Query 执行最近邻查询，按距离升序、距离相同按编号字典序返回最多 K 个结果。
// 距离正好等于 MaxDistance 的要素也算在内。
func (e *Engine) Query(q Query) ([]Result, error) {
	if q.K < 1 || q.K > 100 {
		return nil, fmt.Errorf("K 须在 [1, 100]，当前为 %d", q.K)
	}
	if q.MaxDistance != 0 && (q.MaxDistance < 1 || q.MaxDistance > 50000) {
		return nil, fmt.Errorf("MaxDistance 须在 [1, 50000] 米或为 0（不限），当前为 %v", q.MaxDistance)
	}
	if q.Lon < -180 || q.Lon > 180 || q.Lat < -90 || q.Lat > 90 {
		return nil, fmt.Errorf("查询点坐标越界：(%v, %v)", q.Lon, q.Lat)
	}
	p := lonLatToVec(q.Lon, q.Lat)

	e.mu.RLock()
	defer e.mu.RUnlock()

	h := &candHeap{ids: e.ids}
	threshold := math.Inf(1)
	if q.MaxDistance > 0 {
		threshold = q.MaxDistance / EarthRadius
	}
	cosT, sinT := math.Cos(threshold), math.Sin(threshold)
	piMinusT := math.Pi - threshold
	px, py, pz := p[0], p[1], p[2]

	consider := func(slot int) {
		c := &e.caps[slot]
		// 包围球冠剪枝：angle(p,center) - rad > threshold 时跳过，
		// 即 cos(angle) < cos(threshold+rad) = cosT·cosR - sinT·sinR。
		// threshold + rad >= π 时（rad >= piMinusT）无法剪枝。
		if c.rad < piMinusT && px*c.cx+py*c.cy+pz*c.cz < cosT*c.cosR-sinT*c.sinR {
			return
		}
		d := e.geoms[slot].distToPoint(p)
		if d > threshold {
			return
		}
		heap.Push(h, cand{slot: slot, dist: d})
		if h.Len() > q.K {
			heap.Pop(h)
		}
		if h.Len() == q.K && h.c[0].dist < threshold {
			threshold = h.c[0].dist
			cosT, sinT = math.Cos(threshold), math.Sin(threshold)
			piMinusT = math.Pi - threshold
		}
	}

	if len(q.Layers) == 0 {
		for slot := range e.caps {
			consider(slot)
		}
	} else {
		seen := make(map[string]struct{}, len(q.Layers))
		for _, layer := range q.Layers {
			if _, dup := seen[layer]; dup {
				continue
			}
			seen[layer] = struct{}{}
			lid, ok := e.layerIDs[layer]
			if !ok {
				continue
			}
			for _, slot := range e.layerSlots[lid] {
				consider(slot)
			}
		}
	}

	results := make([]Result, h.Len())
	for i, c := range h.c {
		results[i] = Result{
			ID:       e.ids[c.slot],
			Layer:    e.layerNames[e.layerOf[c.slot]],
			Distance: c.dist * EarthRadius,
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Distance != results[j].Distance {
			return results[i].Distance < results[j].Distance
		}
		return results[i].ID < results[j].ID
	})
	return results, nil
}
