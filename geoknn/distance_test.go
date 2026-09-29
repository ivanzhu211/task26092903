package geoknn

import (
	"math"
	"testing"
)

// distOf 解析 WKT 并计算到 (lon, lat) 的距离（米）。
func distOf(t *testing.T, wkt string, lon, lat float64) float64 {
	t.Helper()
	raw, err := parseWKT(wkt)
	if err != nil {
		t.Fatalf("parseWKT(%q): %v", wkt, err)
	}
	g, err := buildGeometry(raw)
	if err != nil {
		t.Fatalf("buildGeometry(%q): %v", wkt, err)
	}
	return g.distToPoint(lonLatToVec(lon, lat)) * EarthRadius
}

// 一度大圆弧对应的米数。
const deg1 = EarthRadius * math.Pi / 180

func TestDistHandComputable(t *testing.T) {
	cases := []struct {
		name string
		wkt  string
		lon  float64
		lat  float64
		want float64
	}{
		{"赤道上两点", "POINT(1 0)", 0, 0, deg1},
		{"同一经线上两点", "POINT(30 1)", 30, 0, deg1},
		{"赤道上反向", "POINT(-179 0)", 180, 0, deg1},
		{"极点到赤道", "POINT(0 90)", 120, 0, EarthRadius * math.Pi / 2},
		{"南极点", "POINT(50 -90)", -50, 0, EarthRadius * math.Pi / 2},
		{"对跖点", "POINT(0 0)", 180, 0, EarthRadius * math.Pi},
		{"点到赤道线段内垂足", "LINESTRING(0 0, 10 0)", 5, 1, deg1},
		{"点到赤道线段端点", "LINESTRING(0 0, 10 0)", 12, 0, 2 * deg1},
		{"点到经线段内垂足", "LINESTRING(10 0, 10 10)", 10, 5, 0},
		{"跨180经线的线", "LINESTRING(179 0, -179 0)", 180, 0, 0},
		{"跨180经线的线-北侧", "LINESTRING(179 0, -179 0)", 180, 1, deg1},
		{"经度180与-180相同", "POINT(180 5)", -180, 6, deg1},
		{"纬度90经度无关", "POINT(90 90)", -120, 89, deg1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := distOf(t, c.wkt, c.lon, c.lat)
			if math.Abs(got-c.want) > 1e-3 { // 1 毫米
				t.Errorf("dist = %v m, want %v m (diff %v m)", got, c.want, math.Abs(got-c.want))
			}
		})
	}
}

func TestDistSubMeter(t *testing.T) {
	// 赤道上 0.00001 度 ≈ 1.11195 米
	got := distOf(t, "POINT(0.00001 0)", 0, 0)
	want := deg1 * 0.00001
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("dist = %v, want %v", got, want)
	}
	// 高纬度短距离：经度方向 0.0001 度，纬度 60°，约 111.195 * 0.0001 * cos(60°)
	got = distOf(t, "POINT(0.0001 60)", 0, 60)
	want = deg1 * 0.0001 * math.Cos(60*math.Pi/180)
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("dist = %v, want %v", got, want)
	}
}

func TestDistPolygon(t *testing.T) {
	// 外环沿赤道与经线，便于手算：lon∈[0,10], lat∈[0,10]
	outer := "POLYGON((0 0, 10 0, 10 10, 0 10, 0 0))"
	cases := []struct {
		name string
		wkt  string
		lon  float64
		lat  float64
		want float64
	}{
		{"面内点", outer, 5, 5, 0},
		{"边界上的点", outer, 5, 0, 0},
		{"顶点上的点", outer, 0, 0, 0},
		{"面外靠赤道边", outer, 5, -1, deg1},
		{"面外靠经线边", outer, -1, 5, EarthRadius * math.Asin(math.Cos(5*math.Pi/180)*math.Sin(math.Pi/180))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := distOf(t, c.wkt, c.lon, c.lat)
			if math.Abs(got-c.want) > 1e-3 {
				t.Errorf("dist = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDistPolygonWithHole(t *testing.T) {
	// 外环 lon∈[0,10] lat∈[0,10]；洞 lon∈[4,6] lat∈[0,2]，洞底边在赤道上
	wkt := "POLYGON((0 0, 10 0, 10 10, 0 10, 0 0), (4 0, 6 0, 6 2, 4 2, 4 0))"
	// 洞内点 (5,1)：最近的是经线 lon=4/6 的洞边，asin(cos(1°)·sin(1°))
	want := EarthRadius * math.Asin(math.Cos(math.Pi/180)*math.Sin(math.Pi/180))
	if got := distOf(t, wkt, 5, 1); math.Abs(got-want) > 1e-3 {
		t.Errorf("洞内点 dist = %v, want %v", got, want)
	}
	// 洞内点 (5,1.9)：到洞顶边更近；顶边不是大圆沿纬线，只验证非零且小于到外边界的距离
	if got := distOf(t, wkt, 5, 1.9); got <= 0 || got > deg1 {
		t.Errorf("洞内点 dist = %v, 应在 (0, %v) 内", got, deg1)
	}
	// 洞边界上的点 → 0
	if got := distOf(t, wkt, 5, 0); got > 1e-6 {
		t.Errorf("洞边界点 dist = %v, want 0", got)
	}
	// 面上点 → 0
	if got := distOf(t, wkt, 1, 5); got > 1e-6 {
		t.Errorf("面上点 dist = %v, want 0", got)
	}
}

func TestDistPolygonAroundPole(t *testing.T) {
	// 纬圈 80° 围成的面包住北极
	wkt := "POLYGON((0 80, 90 80, 180 80, -90 80, 0 80))"
	if got := distOf(t, wkt, 0, 90); got > 1e-6 {
		t.Errorf("极点在面内 dist = %v, want 0", got)
	}
	if got := distOf(t, wkt, 45, 85); got > 1e-6 {
		t.Errorf("面内点 dist = %v, want 0", got)
	}
	// 面外点 (0, 70)：到纬圈 10 度（经线方向是大圆）
	if got := distOf(t, wkt, 0, 70); math.Abs(got-10*deg1) > 1e-3 {
		t.Errorf("面外点 dist = %v, want %v", got, 10*deg1)
	}
}

func TestDistAntimeridianPolygon(t *testing.T) {
	// 跨 180 经线的面：lon∈[170,-170]（经过 180），lat∈[0,10]
	wkt := "POLYGON((170 0, -170 0, -170 10, 170 10, 170 0))"
	if got := distOf(t, wkt, 180, 5); got > 1e-6 {
		t.Errorf("面内点(180) dist = %v, want 0", got)
	}
	if got := distOf(t, wkt, -175, 5); got > 1e-6 {
		t.Errorf("面内点(-175) dist = %v, want 0", got)
	}
	// 面外点 (160, 5)：到经线 170 的边，asin(cos(5°)·sin(10°))
	want := EarthRadius * math.Asin(math.Cos(5*math.Pi/180)*math.Sin(10*math.Pi/180))
	if got := distOf(t, wkt, 160, 5); math.Abs(got-want) > 1e-3 {
		t.Errorf("面外点 dist = %v, want %v", got, want)
	}
}

func TestDistMulti(t *testing.T) {
	if got := distOf(t, "MULTIPOINT((10 0), (20 0))", 0, 0); math.Abs(got-10*deg1) > 1e-3 {
		t.Errorf("MULTIPOINT dist = %v, want %v", got, 10*deg1)
	}
	if got := distOf(t, "MULTILINESTRING((0 0, 10 0), (20 0, 20 10))", 5, 1); math.Abs(got-deg1) > 1e-3 {
		t.Errorf("MULTILINESTRING dist = %v, want %v", got, deg1)
	}
	wkt := "MULTIPOLYGON(((0 0, 1 0, 1 1, 0 1, 0 0)), ((10 0, 11 0, 11 1, 10 1, 10 0)))"
	if got := distOf(t, wkt, 0.5, 0.5); got > 1e-6 {
		t.Errorf("MULTIPOLYGON 面内 dist = %v, want 0", got)
	}
	if got := distOf(t, wkt, 5, 0); math.Abs(got-4*deg1) > 1e-3 {
		t.Errorf("MULTIPOLYGON 面外 dist = %v, want %v", got, 4*deg1)
	}
}
