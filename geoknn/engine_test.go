package geoknn

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestImportValidation(t *testing.T) {
	e := NewEngine()
	// 正常导入
	err := e.Import([]Feature{
		{ID: "a", Layer: "L1", WKT: "POINT(0 0)"},
		{ID: "b", Layer: "L1", WKT: "POINT(1 1)"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// 批内编号重复 → 整批不生效，报告第 2 个
	err = e.Import([]Feature{
		{ID: "c", Layer: "L1", WKT: "POINT(2 2)"},
		{ID: "c", Layer: "L1", WKT: "POINT(3 3)"},
	})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Index != 1 {
		t.Fatalf("应报告第 1 个（0 基）编号重复，得到 %v", err)
	}
	if got, _ := e.Query(Query{Lon: 2, Lat: 2, K: 100}); len(got) != 2 {
		t.Fatalf("整批应不生效，现有 %d 个要素", len(got))
	}
	// 与已有编号重复
	err = e.Import([]Feature{{ID: "a", Layer: "L1", WKT: "POINT(5 5)"}})
	if !errors.As(err, &ie) || ie.Index != 0 {
		t.Fatalf("应报告与已有编号重复，得到 %v", err)
	}
	// 第 3 个要素 WKT 非法 → 整批不生效
	err = e.Import([]Feature{
		{ID: "d", Layer: "L1", WKT: "POINT(0 0)"},
		{ID: "e", Layer: "L1", WKT: "POINT(1 1)"},
		{ID: "f", Layer: "L1", WKT: "POINT(999 0)"},
	})
	if !errors.As(err, &ie) || ie.Index != 2 || ie.ID != "f" {
		t.Fatalf("应报告第 2 个（0 基）要素非法，得到 %v", err)
	}
	if got, _ := e.Query(Query{Lon: 0, Lat: 0, K: 100}); len(got) != 2 {
		t.Fatalf("整批应不生效，现有 %d 个要素", len(got))
	}
	// 批量超限
	big := make([]Feature, maxBatch+1)
	for i := range big {
		big[i] = Feature{ID: fmt.Sprintf("x%d", i), Layer: "L1", WKT: "POINT(0 0)"}
	}
	if err := e.Import(big); err == nil {
		t.Fatal("超过 5000 个应报错")
	}
	// 空编号
	if err := e.Import([]Feature{{ID: "", Layer: "L1", WKT: "POINT(0 0)"}}); err == nil {
		t.Fatal("空编号应报错")
	}
}

func TestDeleteAndReplace(t *testing.T) {
	e := NewEngine()
	if err := e.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在的编号应报 ErrNotFound，得到 %v", err)
	}
	if err := e.ReplaceGeometry("nope", "POINT(0 0)"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("替换不存在的编号应报 ErrNotFound，得到 %v", err)
	}
	e.Import([]Feature{{ID: "a", Layer: "L1", WKT: "POINT(0 0)"}})
	// 替换为非法几何 → 报错且原几何不变
	if err := e.ReplaceGeometry("a", "POINT(999 0)"); err == nil {
		t.Fatal("替换为非法几何应报错")
	}
	got, _ := e.Query(Query{Lon: 0, Lat: 0, K: 1})
	if len(got) != 1 || got[0].Distance != 0 {
		t.Fatalf("替换失败后原几何应保留，得到 %+v", got)
	}
	// 合法替换立即生效
	if err := e.ReplaceGeometry("a", "POINT(10 0)"); err != nil {
		t.Fatalf("ReplaceGeometry: %v", err)
	}
	got, _ = e.Query(Query{Lon: 10, Lat: 0, K: 1})
	if len(got) != 1 || got[0].Distance > 1e-6 {
		t.Fatalf("替换后应立即可查，得到 %+v", got)
	}
	// 删除立即生效
	if err := e.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = e.Query(Query{Lon: 10, Lat: 0, K: 1})
	if len(got) != 0 {
		t.Fatalf("删除后应查不到，得到 %+v", got)
	}
	// 删除后可重新导入同编号
	if err := e.Import([]Feature{{ID: "a", Layer: "L1", WKT: "POINT(1 1)"}}); err != nil {
		t.Fatalf("删除后重新导入: %v", err)
	}
}

func TestQueryParams(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{{ID: "a", Layer: "L1", WKT: "POINT(0 0)"}})
	for _, q := range []Query{
		{Lon: 0, Lat: 0, K: 0},
		{Lon: 0, Lat: 0, K: 101},
		{Lon: 0, Lat: 0, K: 1, MaxDistance: 0.5},
		{Lon: 0, Lat: 0, K: 1, MaxDistance: 50001},
		{Lon: 181, Lat: 0, K: 1},
		{Lon: 0, Lat: -91, K: 1},
	} {
		if _, err := e.Query(q); err == nil {
			t.Errorf("Query(%+v) 应报参数错误", q)
		}
	}
	// 合法边界
	if _, err := e.Query(Query{Lon: 180, Lat: 90, K: 1, MaxDistance: 1}); err != nil {
		t.Errorf("边界参数应合法: %v", err)
	}
	if _, err := e.Query(Query{Lon: -180, Lat: -90, K: 100, MaxDistance: 50000}); err != nil {
		t.Errorf("边界参数应合法: %v", err)
	}
}

func TestQueryMaxDistanceInclusive(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{
		{ID: "near", Layer: "L1", WKT: "POINT(0 0)"},
		{ID: "far", Layer: "L1", WKT: "POINT(0.1 0)"},
	})
	got, _ := e.Query(Query{Lon: 0, Lat: 0, K: 100})
	d := got[1].Distance // far 的精确距离
	// 正好等于最大距离 → 算在内
	got, _ = e.Query(Query{Lon: 0, Lat: 0, K: 100, MaxDistance: d})
	if len(got) != 2 {
		t.Fatalf("MaxDistance 等于距离时应包含，得到 %d 个", len(got))
	}
	// 差 1 厘米 → 不包含
	got, _ = e.Query(Query{Lon: 0, Lat: 0, K: 100, MaxDistance: d - 0.01})
	if len(got) != 1 {
		t.Fatalf("MaxDistance 小于距离时不应包含，得到 %d 个", len(got))
	}
}

func TestQueryOrdering(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{
		{ID: "b", Layer: "L1", WKT: "POINT(0 0)"},
		{ID: "a", Layer: "L1", WKT: "POINT(0 0)"}, // 与 b 同距离，编号更小
		{ID: "c", Layer: "L1", WKT: "POINT(0.001 0)"},
	})
	got, _ := e.Query(Query{Lon: 0, Lat: 0, K: 100})
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("距离相同应按编号字典序，得到 %+v", got)
	}
	// K 限制
	got, _ = e.Query(Query{Lon: 0, Lat: 0, K: 1})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("K=1 应只返回最近的 a，得到 %+v", got)
	}
}

func TestQueryLayers(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{
		{ID: "w1", Layer: "well", WKT: "POINT(0 0)"},
		{ID: "r1", Layer: "river", WKT: "POINT(0.001 0)"},
		{ID: "p1", Layer: "park", WKT: "POINT(0.002 0)"},
	})
	got, _ := e.Query(Query{Lon: 0, Lat: 0, K: 100, Layers: []string{"river"}})
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("只查 river 图层，得到 %+v", got)
	}
	got, _ = e.Query(Query{Lon: 0, Lat: 0, K: 100, Layers: []string{"well", "park", "well"}})
	if len(got) != 2 || got[0].ID != "w1" || got[1].ID != "p1" {
		t.Fatalf("查两个图层（含重复名），得到 %+v", got)
	}
	got, _ = e.Query(Query{Lon: 0, Lat: 0, K: 100, Layers: []string{"nosuch"}})
	if len(got) != 0 {
		t.Fatalf("不存在的图层应为空，得到 %+v", got)
	}
}

func TestQueryOnBoundary(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{
		{ID: "poly", Layer: "L", WKT: "POLYGON((0 0, 10 0, 10 10, 0 10, 0 0))"},
		{ID: "far", Layer: "L", WKT: "POINT(50 50)"},
	})
	// 正好在顶点、在边上 → 距离 0
	for _, pt := range [][2]float64{{0, 0}, {5, 0}, {10, 5}, {0, 10}} {
		got, err := e.Query(Query{Lon: pt[0], Lat: pt[1], K: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "poly" || got[0].Distance > 1e-6 {
			t.Errorf("边界点 %v 应命中 poly 距离 0，得到 %+v", pt, got)
		}
	}
}

func TestQueryNearPoleAndAntimeridian(t *testing.T) {
	e := NewEngine()
	e.Import([]Feature{
		{ID: "amline", Layer: "L", WKT: "LINESTRING(170 0, -170 0)"},
		{ID: "np", Layer: "L", WKT: "POINT(0 89)"},
		{ID: "eq", Layer: "L", WKT: "POINT(0 0)"},
	})
	// 180 经线上的查询：赤道上命中跨线 amline，距离 0
	got, _ := e.Query(Query{Lon: 180, Lat: 0, K: 1})
	if len(got) != 1 || got[0].ID != "amline" || got[0].Distance != 0 {
		t.Fatalf("180 经线上应命中 amline 距离 0，得到 %+v", got)
	}
	// -180 经线同一条
	got, _ = e.Query(Query{Lon: -180, Lat: 0, K: 1})
	if len(got) != 1 || got[0].ID != "amline" || got[0].Distance > 1e-6 {
		t.Fatalf("-180 经线上应命中 amline 距离 0，得到 %+v", got)
	}
	// 极点附近查询：最近的是 np（到 (180,89) 经极点 2 度）
	got, _ = e.Query(Query{Lon: 180, Lat: 89, K: 1})
	if len(got) != 1 || got[0].ID != "np" {
		t.Fatalf("极点附近应命中 np，得到 %+v", got)
	}
	want := 2 * EarthRadius * math.Pi / 180
	if math.Abs(got[0].Distance-want) > 1e-3 {
		t.Fatalf("np 距离应为 %v，得到 %v", want, got[0].Distance)
	}
	// 极点上的查询
	got, _ = e.Query(Query{Lon: 77, Lat: 90, K: 1})
	if len(got) != 1 || got[0].ID != "np" {
		t.Fatalf("极点上应命中 np，得到 %+v", got)
	}
}
