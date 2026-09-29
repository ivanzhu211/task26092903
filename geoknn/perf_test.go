package geoknn

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// 满规模性能测试：50 图层、50 万要素、约 350 万顶点（含 50k 顶点的大要素）。
// 单独运行：go test -run TestPerfFullScale -v
func TestPerfFullScale(t *testing.T) {
	if testing.Short() {
		t.Skip("满规模性能测试，用 -short 时跳过")
	}
	rng := rand.New(rand.NewSource(42))
	e := NewEngine()

	const total = 500000
	const layers = 50

	genWKT := func(i int) string {
		lon := -180 + 360*rng.Float64()
		lat := -90 + 180*rng.Float64()
		switch {
		case i%10000 == 9999: // 每万个一个 5 万顶点的大线（精细海岸线）
			pts := fmt.Sprintf("%f %f", lon, lat)
			for j := 1; j < 50000; j++ {
				lon += (rng.Float64() - 0.5) * 0.01
				lat += (rng.Float64() - 0.5) * 0.01
				if lat > 89 {
					lat = 89
				}
				if lat < -89 {
					lat = -89
				}
				pts += fmt.Sprintf(", %f %f", wrapLon(lon), lat)
			}
			return "LINESTRING(" + pts + ")"
		case i%5 == 0: // 20% 线，平均 12 顶点
			n := 2 + rng.Intn(22)
			pts := fmt.Sprintf("%f %f", lon, lat)
			for j := 1; j < n; j++ {
				lon += (rng.Float64() - 0.5) * 2
				lat += (rng.Float64() - 0.5) * 2
				pts += fmt.Sprintf(", %f %f", wrapLon(lon), clampLat(lat))
			}
			return "LINESTRING(" + pts + ")"
		case i%5 == 1: // 20% 面，外环 8 顶点
			pts := ""
			n := 8
			for j := 0; j < n; j++ {
				a := 2 * 3.14159265 * float64(j) / float64(n)
				d := 0.5 + rng.Float64()
				if j > 0 {
					pts += ", "
				}
				pts += fmt.Sprintf("%f %f", wrapLon(lon+d*1.4*math.Cos(a)), clampLat(lat+d*math.Sin(a)))
			}
			// 闭合
			pts += ", " + pts[:indexOf(pts, ",")]
			return "POLYGON((" + pts + "))"
		default: // 60% 点
			return fmt.Sprintf("POINT(%f %f)", lon, lat)
		}
	}

	// 分批导入，统计每批耗时
	var maxBatchDur time.Duration
	start := time.Now()
	for base := 0; base < total; base += maxBatch {
		n := maxBatch
		if base+n > total {
			n = total - base
		}
		batch := make([]Feature, 0, n)
		for i := 0; i < n; i++ {
			id := base + i
			batch = append(batch, Feature{
				ID:    fmt.Sprintf("f%06d", id),
				Layer: fmt.Sprintf("layer%02d", id%layers),
				WKT:   genWKT(id),
			})
		}
		bs := time.Now()
		if err := e.Import(batch); err != nil {
			t.Fatalf("Import 第 %d 批: %v", base/maxBatch, err)
		}
		if d := time.Since(bs); d > maxBatchDur {
			maxBatchDur = d
		}
	}
	t.Logf("导入 %d 个要素总耗时 %v，单批最大 %v（顶点 %d）", total, time.Since(start), maxBatchDur, e.vertexCount)
	if e.vertexCount > maxVertices {
		t.Fatalf("顶点数 %d 超限", e.vertexCount)
	}

	// 查询：混合任意点、两极、180 经线附近、单图层、多图层、带最大距离
	var totalQ, maxQ time.Duration
	const numQ = 2000
	for i := 0; i < numQ; i++ {
		q := Query{K: 1 + rng.Intn(100)}
		switch i % 10 {
		case 0: // 两极附近
			q.Lon = -180 + 360*rng.Float64()
			q.Lat = 89 + rng.Float64()
			if rng.Intn(2) == 0 {
				q.Lat = -q.Lat
			}
		case 1: // 180 经线附近
			q.Lon = 179 + rng.Float64()
			if rng.Intn(2) == 0 {
				q.Lon = -q.Lon
			}
			q.Lat = -90 + 180*rng.Float64()
		case 2: // 单个小图层（1/50 数据）
			q.Lon = -180 + 360*rng.Float64()
			q.Lat = -90 + 180*rng.Float64()
			q.Layers = []string{fmt.Sprintf("layer%02d", rng.Intn(layers))}
		case 3: // 带最大距离
			q.Lon = -180 + 360*rng.Float64()
			q.Lat = -90 + 180*rng.Float64()
			q.MaxDistance = 1 + rng.Float64()*49999
		case 4: // 三个图层
			q.Lon = -180 + 360*rng.Float64()
			q.Lat = -90 + 180*rng.Float64()
			for j := 0; j < 3; j++ {
				q.Layers = append(q.Layers, fmt.Sprintf("layer%02d", rng.Intn(layers)))
			}
		default:
			q.Lon = -180 + 360*rng.Float64()
			q.Lat = -90 + 180*rng.Float64()
		}
		qs := time.Now()
		if _, err := e.Query(q); err != nil {
			t.Fatalf("查询 %+v: %v", q, err)
		}
		d := time.Since(qs)
		totalQ += d
		if d > maxQ {
			maxQ = d
		}
	}
	avg := totalQ / numQ
	t.Logf("查询 %d 次：平均 %v，最慢 %v", numQ, avg, maxQ)
	if avg > 10*time.Millisecond {
		t.Errorf("平均查询耗时 %v 超过 10ms", avg)
	}
	if maxQ > 50*time.Millisecond {
		t.Errorf("最慢查询耗时 %v 超过 50ms", maxQ)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("堆内存占用 %.1f MB", float64(ms.HeapAlloc)/1024/1024)
	if ms.HeapAlloc > 2<<30 {
		t.Errorf("堆内存 %d 字节超过 2GB", ms.HeapAlloc)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// 大批量导入性能：5000 个要素、合计 50 万顶点，2 秒内完成。
func TestPerfHeavyBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("用 -short 时跳过")
	}
	e := NewEngine()
	rng := rand.New(rand.NewSource(7))
	batch := make([]Feature, 0, maxBatch)
	for i := 0; i < maxBatch; i++ {
		lon := -180 + 360*rng.Float64()
		lat := -90 + 180*rng.Float64()
		pts := fmt.Sprintf("%f %f", lon, lat)
		for j := 1; j < 100; j++ { // 每个 100 顶点，合计 50 万
			lon += (rng.Float64() - 0.5) * 0.5
			lat += (rng.Float64() - 0.5) * 0.5
			pts += fmt.Sprintf(", %f %f", wrapLon(lon), clampLat(lat))
		}
		batch = append(batch, Feature{
			ID:    fmt.Sprintf("h%05d", i),
			Layer: "heavy",
			WKT:   "LINESTRING(" + pts + ")",
		})
	}
	start := time.Now()
	if err := e.Import(batch); err != nil {
		t.Fatal(err)
	}
	d := time.Since(start)
	t.Logf("5000 要素 / %d 顶点导入耗时 %v", e.vertexCount, d)
	if e.vertexCount != 500000 {
		t.Fatalf("顶点数 %d，期望 500000", e.vertexCount)
	}
	if d > 2*time.Second {
		t.Errorf("导入耗时 %v 超过 2 秒", d)
	}
}
