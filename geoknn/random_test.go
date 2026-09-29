package geoknn

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

// ---------- 随机数据生成 ----------

func wrapLon(lon float64) float64 {
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	return lon - 180
}

func clampLat(lat float64) float64 {
	if lat > 89.9 {
		return 89.9
	}
	if lat < -89.9 {
		return -89.9
	}
	return lat
}

func fmtPt(lon, lat float64) string {
	return fmt.Sprintf("%.10f %.10f", lon, lat)
}

// genPoint 生成随机点，返回 WKT 与一个代表点（用于边界/近距离查询）。
func genPoint(rng *rand.Rand) (string, [2]float64) {
	lon := -180 + 360*rng.Float64()
	lat := -90 + 180*rng.Float64()
	return "POINT(" + fmtPt(lon, lat) + ")", [2]float64{lon, lat}
}

// genLine 生成随机折线；nearAntimeridian 时从 180 经线附近向东跨过去。
func genLine(rng *rand.Rand, nearAntimeridian bool) (string, [2]float64) {
	n := 2 + rng.Intn(15)
	var lon float64
	if nearAntimeridian {
		lon = 178 + 2*rng.Float64()
	} else {
		lon = -180 + 360*rng.Float64()
	}
	lat := -85 + 170*rng.Float64()
	first := [2]float64{wrapLon(lon), lat}
	pts := "(" + fmtPt(first[0], first[1])
	for i := 1; i < n; i++ {
		if nearAntimeridian {
			lon += rng.Float64() * 2 // 向东跨 180
		} else {
			lon += (rng.Float64() - 0.5) * 8
		}
		lat = clampLat(lat + (rng.Float64()-0.5)*4)
		pts += ", " + fmtPt(wrapLon(lon), lat)
	}
	return "LINESTRING" + pts + ")", first
}

// genRing 生成围绕 (clon, clat) 的简单环（按方位角排序保证不自交）。
func genRing(rng *rand.Rand, clon, clat, radiusDeg float64, n int, minF, maxF float64) [][2]float64 {
	bearings := make([]float64, n)
	for i := range bearings {
		bearings[i] = 2 * math.Pi * rng.Float64()
	}
	sort.Float64s(bearings)
	pts := make([][2]float64, 0, n+1)
	for _, b := range bearings {
		d := radiusDeg * (minF + (maxF-minF)*rng.Float64())
		lat := clampLat(clat + d*math.Cos(b))
		cosLat := math.Cos(clat * math.Pi / 180)
		if cosLat < 0.1 {
			cosLat = 0.1
		}
		lon := wrapLon(clon + d*math.Sin(b)/cosLat)
		pts = append(pts, [2]float64{lon, lat})
	}
	pts = append(pts, pts[0])
	return pts
}

func ringWKT(pts [][2]float64) string {
	s := "("
	for i, p := range pts {
		if i > 0 {
			s += ", "
		}
		s += fmtPt(p[0], p[1])
	}
	return s + ")"
}

// genPolygon 生成随机面（可带洞、跨 180 经线、包住极点）。
func genPolygon(rng *rand.Rand, kind int) (string, [2]float64) {
	var clon, clat float64
	switch kind {
	case 1: // 跨 180 经线
		clon = 180 - 2*rng.Float64()
		if rng.Intn(2) == 0 {
			clon = -clon
		}
		clat = -70 + 140*rng.Float64()
	case 2: // 包住极点
		clat = 90.0
		if rng.Intn(2) == 0 {
			clat = -90.0
		}
		clon = 0
	default:
		clon = -180 + 360*rng.Float64()
		clat = -80 + 160*rng.Float64()
	}
	radius := 0.5 + 4.5*rng.Float64()
	nv := 3 + rng.Intn(8)
	var outer [][2]float64
	if kind == 2 {
		// 绕极点一圈：方位角即经度、纬度接近极点，保证环简单且包住极点
		bearings := make([]float64, nv)
		for i := range bearings {
			bearings[i] = 360 * rng.Float64()
		}
		sort.Float64s(bearings)
		for _, b := range bearings {
			d := radius * (0.6 + 0.4*rng.Float64())
			lat := clat
			if clat > 0 {
				lat -= d
			} else {
				lat += d
			}
			outer = append(outer, [2]float64{wrapLon(b), lat})
		}
		outer = append(outer, outer[0])
	} else {
		outer = genRing(rng, clon, clat, radius, nv, 0.6, 1.0)
	}
	wkt := "POLYGON(" + ringWKT(outer)
	if rng.Intn(3) == 0 && kind != 2 {
		// 加一个洞：同中心、半径明显更小，保证在外环内
		hole := genRing(rng, clon, clat, radius, 3+rng.Intn(4), 0.1, 0.3)
		wkt += ", " + ringWKT(hole)
	}
	return wkt + ")", outer[0]
}

// genFeature 生成一个随机要素的 WKT 与代表点。
func genFeature(rng *rand.Rand) (string, [2]float64) {
	switch rng.Intn(12) {
	case 0, 1, 2, 3:
		return genPoint(rng)
	case 4, 5:
		return genLine(rng, false)
	case 6:
		return genLine(rng, true)
	case 7:
		return genPolygon(rng, 1)
	case 8:
		return genPolygon(rng, 2)
	case 9:
		return genPolygon(rng, 0)
	case 10: // MULTIPOINT / MULTILINESTRING
		if rng.Intn(2) == 0 {
			w1, p1 := genPoint(rng)
			w2, _ := genPoint(rng)
			body := func(w string) string { return "(" + w[len("POINT("):] }
			return "MULTIPOINT(" + body(w1) + ", " + body(w2) + ")", p1
		}
		w1, p1 := genLine(rng, rng.Intn(2) == 0)
		w2, _ := genLine(rng, false)
		body := func(w string) string { return w[len("LINESTRING"):] }
		return "MULTILINESTRING(" + body(w1) + ", " + body(w2) + ")", p1
	default: // MULTIPOLYGON
		w1, p1 := genPolygon(rng, rng.Intn(3))
		w2, _ := genPolygon(rng, 0)
		body := func(w string) string { return w[len("POLYGON"):] }
		return "MULTIPOLYGON(" + body(w1) + ", " + body(w2) + ")", p1
	}
}

// ---------- 蛮力参考 ----------

type featureRec struct {
	layer string
	geom  *geometry
}

// bruteForce 逐个要素算距离，返回期望结果。
func bruteForce(model map[string]featureRec, q Query) []Result {
	p := lonLatToVec(q.Lon, q.Lat)
	layerOK := func(layer string) bool {
		if len(q.Layers) == 0 {
			return true
		}
		for _, l := range q.Layers {
			if l == layer {
				return true
			}
		}
		return false
	}
	var all []Result
	for id, rec := range model {
		if !layerOK(rec.layer) {
			continue
		}
		d := rec.geom.distToPoint(p) * EarthRadius
		if q.MaxDistance > 0 && d > q.MaxDistance {
			continue
		}
		all = append(all, Result{ID: id, Layer: rec.layer, Distance: d})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Distance != all[j].Distance {
			return all[i].Distance < all[j].Distance
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > q.K {
		all = all[:q.K]
	}
	return all
}

// compareResults 对比结果：距离相差不超过 1 毫米的要素之间次序可以不同。
func compareResults(t *testing.T, q Query, want, got []Result) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("查询 %+v：期望 %d 个结果，得到 %d 个\nwant=%v\ngot=%v", q, len(want), len(got), want, got)
	}
	// got 必须按（距离，编号）排好
	for i := 1; i < len(got); i++ {
		if got[i].Distance < got[i-1].Distance ||
			(got[i].Distance == got[i-1].Distance && got[i].ID < got[i-1].ID) {
			t.Fatalf("查询 %+v：结果未排序：%v", q, got)
		}
	}
	// 距离序列一致（1 毫米内），ID 多重集合一致
	idsWant := make(map[string]int)
	idsGot := make(map[string]int)
	for i := range want {
		if math.Abs(want[i].Distance-got[i].Distance) > 1e-3 {
			t.Fatalf("查询 %+v：第 %d 个距离 %v vs %v\nwant=%v\ngot=%v", q, i, want[i].Distance, got[i].Distance, want, got)
		}
		idsWant[want[i].ID]++
		idsGot[got[i].ID]++
	}
	for id, n := range idsWant {
		if idsGot[id] != n {
			t.Fatalf("查询 %+v：要素 %s 次数 %d vs %d\nwant=%v\ngot=%v", q, id, n, idsGot[id], want, got)
		}
	}
}

// ---------- 随机对比测试 ----------

var testLayers = []string{"well", "river", "park", "road", "boundary"}

func TestRandomCompare(t *testing.T) {
	rng := rand.New(rand.NewSource(20260930))
	e := NewEngine()
	model := make(map[string]featureRec)
	nextID := 0
	var ids []string

	importBatch := func(n int) {
		batch := make([]Feature, 0, n)
		recs := make(map[string]featureRec)
		for i := 0; i < n; i++ {
			wkt, _ := genFeature(rng)
			id := fmt.Sprintf("f%06d", nextID)
			nextID++
			layer := testLayers[rng.Intn(len(testLayers))]
			batch = append(batch, Feature{ID: id, Layer: layer, WKT: wkt})
			raw, err := parseWKT(wkt)
			if err != nil {
				t.Fatalf("生成的 WKT 解析失败 %q: %v", wkt, err)
			}
			g, err := buildGeometry(raw)
			if err != nil {
				t.Fatalf("生成的几何不合法 %q: %v", wkt, err)
			}
			recs[id] = featureRec{layer: layer, geom: g}
		}
		if err := e.Import(batch); err != nil {
			t.Fatalf("Import: %v", err)
		}
		for id, rec := range recs {
			model[id] = rec
			ids = append(ids, id)
		}
	}

	// 初始导入 3000 个，分批
	for len(ids) < 3000 {
		importBatch(100 + rng.Intn(400))
	}

	// 查询点生成：覆盖任意位置、两极、180 经线、边界上、1 米以内
	queryPoint := func() (float64, float64) {
		switch rng.Intn(10) {
		case 0: // 两极附近
			lat := 89 + rng.Float64()
			if rng.Intn(2) == 0 {
				lat = -lat
			}
			return -180 + 360*rng.Float64(), lat
		case 1: // 180 经线附近
			lon := 179 + rng.Float64()
			if rng.Intn(2) == 0 {
				lon = -lon
			}
			return lon, -90 + 180*rng.Float64()
		case 2: // 正好在某个要素顶点上（边界上）
			if len(ids) == 0 {
				return 0, 0
			}
			rec := model[ids[rng.Intn(len(ids))]]
			var v vec3
			rec.geom.forEachVertex(func(x vec3) { v = x })
			lat := math.Asin(v[2]) * 180 / math.Pi
			lon := math.Atan2(v[1], v[0]) * 180 / math.Pi
			return lon, lat
		case 3: // 距某顶点 1 米以内
			if len(ids) == 0 {
				return 0, 0
			}
			rec := model[ids[rng.Intn(len(ids))]]
			var v vec3
			rec.geom.forEachVertex(func(x vec3) { v = x })
			lat := math.Asin(v[2])*180/math.Pi + (rng.Float64()-0.5)*1e-5
			lon := math.Atan2(v[1], v[0])*180/math.Pi + (rng.Float64()-0.5)*1e-5
			return wrapLon(lon), clampLat(lat)
		default:
			return -180 + 360*rng.Float64(), -90 + 180*rng.Float64()
		}
	}

	runQueries := func(round, n int) {
		for i := 0; i < n; i++ {
			lon, lat := queryPoint()
			q := Query{Lon: lon, Lat: lat, K: 1 + rng.Intn(100)}
			if rng.Intn(3) == 0 {
				q.MaxDistance = 1 + rng.Float64()*49999
			}
			if rng.Intn(3) == 0 {
				nl := 1 + rng.Intn(2)
				for j := 0; j < nl; j++ {
					q.Layers = append(q.Layers, testLayers[rng.Intn(len(testLayers))])
				}
			}
			got, err := e.Query(q)
			if err != nil {
				t.Fatalf("第 %d 轮查询 %+v 报错: %v", round, q, err)
			}
			want := bruteForce(model, q)
			compareResults(t, q, want, got)
		}
	}

	runQueries(0, 50)

	// 随机穿插编辑，每次编辑后重新对比
	for round := 1; round <= 20; round++ {
		// 删除
		for i := 0; i < 1+rng.Intn(5) && len(ids) > 0; i++ {
			idx := rng.Intn(len(ids))
			id := ids[idx]
			if err := e.Delete(id); err != nil {
				t.Fatalf("第 %d 轮 Delete(%s): %v", round, id, err)
			}
			delete(model, id)
			ids = append(ids[:idx], ids[idx+1:]...)
		}
		// 替换几何
		for i := 0; i < 1+rng.Intn(5) && len(ids) > 0; i++ {
			id := ids[rng.Intn(len(ids))]
			wkt, _ := genFeature(rng)
			if err := e.ReplaceGeometry(id, wkt); err != nil {
				t.Fatalf("第 %d 轮 ReplaceGeometry(%s): %v", round, id, err)
			}
			raw, _ := parseWKT(wkt)
			g, err := buildGeometry(raw)
			if err != nil {
				t.Fatalf("生成的几何不合法 %q: %v", wkt, err)
			}
			rec := model[id]
			rec.geom = g
			model[id] = rec
		}
		// 导入
		if rng.Intn(2) == 0 {
			importBatch(10 + rng.Intn(40))
		}
		runQueries(round, 20)
	}
	t.Logf("最终要素数: %d", len(ids))
}
