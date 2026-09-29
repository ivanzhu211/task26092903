package geoknn

import (
	"math"
	"math/rand"
	"testing"
)

// 剪枝不变式：distToPoint(p) >= max(0, angle(p,center) - radius)。
// 这是包围球冠剪枝正确性的根基，违反它会导致查询漏结果。
func TestCapInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		wkt, _ := genFeature(rng)
		raw, err := parseWKT(wkt)
		if err != nil {
			t.Fatalf("parse %q: %v", wkt, err)
		}
		g, err := buildGeometry(raw)
		if err != nil {
			t.Fatalf("build %q: %v", wkt, err)
		}
		center, radius := computeCap(g)
		if radius >= math.Pi/2 {
			continue // 引擎对角半径 >= π/2 的要素不剪枝，无需不变式
		}
		for j := 0; j < 20; j++ {
			var p vec3
			if j == 0 && len(g.polys) > 0 {
				p = center // 面内点也要满足
			} else {
				p = lonLatToVec(-180+360*rng.Float64(), -90+180*rng.Float64())
			}
			d := g.distToPoint(p)
			lb := angleBetween(p, center) - radius
			if lb < 0 {
				lb = 0
			}
			if d < lb-1e-9 {
				lat := math.Asin(p[2]) * 180 / math.Pi
				lon := math.Atan2(p[1], p[0]) * 180 / math.Pi
				t.Fatalf("不变式违反: wkt=%s\n d=%v lb=%v p=(%v,%v)", wkt, d, lb, lon, lat)
			}
		}
	}
}
