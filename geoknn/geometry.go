package geoknn

import "fmt"

// edge 是球面上相邻两顶点间较短的大圆弧。
// n 为 a×b 归一化后的法向量。
type edge struct {
	a, b, n vec3
}

// ring 是闭合环（首尾的重复顶点已去除，边按环形相接）。
// center 是顶点向量之和的方向；环在某一开半球内，故该向量和非零，
// 且 center 的对跖点一定在环所围较小区域之外（用于点在环内判断）。
type ring struct {
	edges  []edge
	center vec3
}

// polygon 中 rings[0] 为外环，其余为洞。
type polygon struct {
	rings []ring
}

// geometry 是校验并预处理后的要素几何。
type geometry struct {
	points []vec3
	lines  [][]edge
	polys  []polygon
}

// samePoint 判断两个单位向量是否为同一点（弦长阈值约 1e-12）。
func samePoint(a, b vec3) bool {
	return vNorm(vSub(a, b)) < 1e-12
}

// checkBounds 校验经纬度范围。
func checkBounds(p rawPoint) error {
	if p[0] < -180 || p[0] > 180 {
		return fmt.Errorf("经度 %v 超出 [-180, 180]", p[0])
	}
	if p[1] < -90 || p[1] > 90 {
		return fmt.Errorf("纬度 %v 超出 [-90, 90]", p[1])
	}
	return nil
}

// toVerts 转换坐标为向量并校验范围、去除相邻重复顶点。
func toVerts(raw []rawPoint) ([]vec3, error) {
	verts := make([]vec3, 0, len(raw))
	for _, rp := range raw {
		if err := checkBounds(rp); err != nil {
			return nil, err
		}
		v := lonLatToVec(rp[0], rp[1])
		if len(verts) > 0 && samePoint(verts[len(verts)-1], v) {
			continue // 相邻重复顶点忽略
		}
		verts = append(verts, v)
	}
	return verts, nil
}

// buildEdges 把顶点序列连成边，校验相邻顶点不对跖。closed 表示首尾再连一条边。
func buildEdges(verts []vec3, closed bool) ([]edge, error) {
	n := len(verts) - 1
	if closed {
		n = len(verts)
	}
	edges := make([]edge, 0, n)
	for i := 0; i < n; i++ {
		a := verts[i]
		b := verts[(i+1)%len(verts)]
		if vDot(a, b) <= -(1 - 1e-12) {
			return nil, fmt.Errorf("相邻顶点正好对跖")
		}
		edges = append(edges, edge{a: a, b: b, n: vNormalize(vCross(a, b))})
	}
	return edges, nil
}

// buildGeometry 校验 rawGeom 并构建 geometry。
// 校验规则：空几何、坐标越界、环不闭合、顶点不够、相邻顶点对跖。
func buildGeometry(g *rawGeom) (*geometry, error) {
	out := &geometry{}
	for _, rp := range g.points {
		if err := checkBounds(rp); err != nil {
			return nil, err
		}
		out.points = append(out.points, lonLatToVec(rp[0], rp[1]))
	}
	for _, rawLine := range g.lines {
		verts, err := toVerts(rawLine)
		if err != nil {
			return nil, err
		}
		if len(verts) < 2 {
			return nil, fmt.Errorf("线至少需要 2 个不同顶点")
		}
		edges, err := buildEdges(verts, false)
		if err != nil {
			return nil, err
		}
		out.lines = append(out.lines, edges)
	}
	for _, rawPoly := range g.polys {
		poly := polygon{}
		for _, rawRing := range rawPoly {
			r, err := buildRing(rawRing)
			if err != nil {
				return nil, err
			}
			poly.rings = append(poly.rings, r)
		}
		out.polys = append(out.polys, poly)
	}
	if len(out.points) == 0 && len(out.lines) == 0 && len(out.polys) == 0 {
		return nil, fmt.Errorf("空几何")
	}
	return out, nil
}

// buildRing 校验并构建一个环：首尾相同、至少 3 个不同顶点。
func buildRing(raw []rawPoint) (ring, error) {
	if len(raw) < 2 {
		return ring{}, fmt.Errorf("环至少需要 3 个不同顶点且首尾相同")
	}
	verts, err := toVerts(raw)
	if err != nil {
		return ring{}, err
	}
	if !samePoint(verts[0], verts[len(verts)-1]) {
		return ring{}, fmt.Errorf("环不闭合")
	}
	verts = verts[:len(verts)-1] // 去掉收尾重复顶点
	// 闭合缝两侧可能也是重复顶点（如 ...A B A B A），继续去除
	for len(verts) > 1 && samePoint(verts[0], verts[len(verts)-1]) {
		verts = verts[:len(verts)-1]
	}
	if len(verts) < 3 {
		return ring{}, fmt.Errorf("环至少需要 3 个不同顶点")
	}
	edges, err := buildEdges(verts, true)
	if err != nil {
		return ring{}, err
	}
	sum := vec3{}
	for _, v := range verts {
		sum = vAdd(sum, v)
	}
	return ring{edges: edges, center: vNormalize(sum)}, nil
}

// vertexCount 返回几何的顶点总数。
func (g *geometry) vertexCount() int {
	n := len(g.points)
	for _, l := range g.lines {
		n += len(l) + 1
	}
	for _, p := range g.polys {
		for _, r := range p.rings {
			n += len(r.edges)
		}
	}
	return n
}
