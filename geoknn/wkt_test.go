package geoknn

import (
	"strings"
	"testing"
)

func TestParseWKTValid(t *testing.T) {
	cases := []string{
		"POINT(30 10)",
		"point ( 30  10 )",
		"POINT(-179.5 89.999)",
		"POINT(1.5e2 -4.5E-1)",
		"MULTIPOINT((10 40), (40 30), (20 20))",
		"MULTIPOINT(10 40, 40 30)",
		"LINESTRING(30 10, 10 30, 40 40)",
		"MULTILINESTRING((10 10, 20 20), (15 15, 30 15, 40 10))",
		"POLYGON((30 10, 40 40, 20 40, 10 20, 30 10))",
		"POLYGON((35 10, 45 45, 15 40, 10 20, 35 10), (20 30, 35 35, 30 20, 20 30))",
		"MULTIPOLYGON(((30 20, 45 40, 10 40, 30 20)), ((15 5, 40 10, 10 20, 5 10, 15 5)))",
		"MULTIPOLYGON(((30 20, 45 40, 10 40, 30 20)), ((15 5, 40 10, 10 20, 15 5), (20 10, 25 15, 22 12, 20 10)))",
		"  Polygon (( 0 0 , 1 0 , 1 1 , 0 0 ))  ",
	}
	for _, c := range cases {
		if _, err := parseWKT(c); err != nil {
			t.Errorf("parseWKT(%q) 报错: %v", c, err)
		}
	}
}

func TestParseWKTInvalid(t *testing.T) {
	cases := []string{
		"",
		"POINT",
		"POINT()",
		"POINT(30)",
		"POINT(30 10 5)",
		"POINT EMPTY",
		"POINT(30 10",
		"POINT(30 10))",
		"POINT(30, 10)",
		"POINT(abc 10)",
		"LINESTRING(30 10 10 30)",
		"POLYGON(30 10, 40 40, 20 40, 30 10)",
		"GEOMETRYCOLLECTION(POINT(1 2))",
		"CIRCLE(1 2, 3)",
		"POINT(30 10) EXTRA",
	}
	for _, c := range cases {
		if _, err := parseWKT(c); err == nil {
			t.Errorf("parseWKT(%q) 应报错", c)
		}
	}
}

func mustBuild(t *testing.T, wkt string) *geometry {
	t.Helper()
	raw, err := parseWKT(wkt)
	if err != nil {
		t.Fatalf("parseWKT(%q): %v", wkt, err)
	}
	g, err := buildGeometry(raw)
	if err != nil {
		t.Fatalf("buildGeometry(%q): %v", wkt, err)
	}
	return g
}

func TestBuildGeometryValid(t *testing.T) {
	// 相邻重复顶点被忽略
	g := mustBuild(t, "LINESTRING(0 0, 0 0, 1 1, 1 1, 2 0)")
	if len(g.lines) != 1 || len(g.lines[0]) != 2 {
		t.Errorf("相邻重复顶点应被忽略，得到 %d 条边", len(g.lines[0]))
	}
	// 环闭合处经度 180 与 -180 视为同一点
	mustBuild(t, "POLYGON((170 0, 180 5, -170 0, 170 0))")
	// 纬度 90 时经度不影响闭合判断
	mustBuild(t, "POLYGON((0 80, 90 90, 180 80, 0 80))")
}

func TestBuildGeometryInvalid(t *testing.T) {
	cases := []struct {
		wkt    string
		reason string
	}{
		{"POINT(181 0)", "经度"},
		{"POINT(-180.0001 0)", "经度"},
		{"POINT(0 90.5)", "纬度"},
		{"POINT(0 -91)", "纬度"},
		{"LINESTRING(0 0, 0 0)", "顶点"},
		{"LINESTRING(5 5, 5 5, 5 5)", "顶点"},
		{"POLYGON((0 0, 1 0, 1 1))", "闭合"},
		{"POLYGON((0 0, 1 0, 0 0))", "顶点"},
		{"LINESTRING(0 0, 180 0)", "对跖"},
		{"LINESTRING(0 0, 10 10, -170 -10)", "对跖"},
		{"POLYGON((0 0, 180 0, 90 45, 0 0))", "对跖"},
	}
	for _, c := range cases {
		raw, err := parseWKT(c.wkt)
		if err != nil {
			t.Fatalf("parseWKT(%q): %v", c.wkt, err)
		}
		_, err = buildGeometry(raw)
		if err == nil {
			t.Errorf("buildGeometry(%q) 应报错（%s）", c.wkt, c.reason)
			continue
		}
		if !strings.Contains(err.Error(), c.reason) {
			t.Errorf("buildGeometry(%q) 错误 %q 应包含 %q", c.wkt, err, c.reason)
		}
	}
}

func TestBuildGeometryPolygonDuplicateSeam(t *testing.T) {
	// 闭合缝两侧的重复顶点也要去除：(0 0, 1 0, 1 1, 0 0) 合法（3 个不同顶点）
	mustBuild(t, "POLYGON((0 0, 1 0, 1 1, 0 0))")
	// 去重后不足 3 个不同顶点
	raw, err := parseWKT("POLYGON((0 0, 1 1, 0 0, 0 0))")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildGeometry(raw); err == nil {
		t.Error("去重后不足 3 个不同顶点应报错")
	}
}
