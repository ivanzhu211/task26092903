package geoknn

import "math"

// distToPoint 返回几何到单位向量 p 的球面距离（弧度）。
// 点在面内或面的边界上时为 0；点落在洞里时按到洞边界的距离算。
func (g *geometry) distToPoint(p vec3) float64 {
	best := math.Inf(1)
	for _, v := range g.points {
		if d := angleBetween(p, v); d < best {
			best = d
		}
	}
	for _, l := range g.lines {
		for _, e := range l {
			if d := pointEdgeDist(p, e); d < best {
				best = d
				if best == 0 {
					return 0
				}
			}
		}
	}
	for i := range g.polys {
		if d := g.polys[i].distToPoint(p); d < best {
			best = d
			if best == 0 {
				return 0
			}
		}
	}
	return best
}

// distToPoint 返回面到 p 的距离（弧度）。在面内或边界上为 0。
func (poly *polygon) distToPoint(p vec3) float64 {
	minEdge := math.Inf(1)
	for _, r := range poly.rings {
		for _, e := range r.edges {
			if d := pointEdgeDist(p, e); d < minEdge {
				minEdge = d
			}
		}
	}
	// 在边界上（阈值约 6 纳米，远小于 1 毫米精度要求）
	if minEdge <= 1e-12 {
		return 0
	}
	// 在外环内且不在任何洞内 → 在面上
	if inRing(p, poly.rings[0]) {
		inside := true
		for _, hole := range poly.rings[1:] {
			if inRing(p, hole) {
				inside = false
				break
			}
		}
		if inside {
			return 0
		}
	}
	// 在面外时最近点在外环上；在洞内时最近点在洞边界上。
	// 两种情况下对所有环的边取最小都正确。
	return minEdge
}

// pointEdgeDist 返回点 p 到大圆弧 e 的球面距离（弧度）。
func pointEdgeDist(p vec3, e edge) float64 {
	d := vDot(p, e.n) // p 到大圆所在平面的有向距离（弦）
	// 大圆上最近点方向：t = p - d·n
	t := vSub(p, vScale(e.n, d))
	// t 落在较短弧 ab 上当且仅当 a→t、t→b 绕 n 均为正转
	if vDot(vCross(e.a, t), e.n) >= 0 && vDot(vCross(t, e.b), e.n) >= 0 {
		s := d
		if s < 0 {
			s = -s
		}
		if s > 1 {
			s = 1
		}
		return math.Asin(s)
	}
	da := angleBetween(p, e.a)
	db := angleBetween(p, e.b)
	if da < db {
		return da
	}
	return db
}

// inRing 判断 p 是否在环所围的较小一侧（不含边界）。
// 用射线法：取环中心的对跖点 q（由环小于半球可证 q 在较小区域之外），
// 统计较短弧 p→q 与环的边的相交次数，奇数次即在内部。
// 调用前已保证 p 不在任何边上（到边距离 > 1e-12）。
func inRing(p vec3, r ring) bool {
	// q 微扰一个固定小量，避免 p 与 ±center 重合时弧退化
	q := vNormalize(vec3{
		-r.center[0] + 1e-7*0.2672612416914244,
		-r.center[1] + 1e-7*0.5345224833828488,
		-r.center[2] + 1e-7*0.8017837257642732,
	})
	pq := vCross(p, q)
	crossings := 0
	for _, e := range r.edges {
		// 两侧条件：a、b 分居大圆 PQ 两侧，且 p、q 分居大圆 AB 两侧
		if vDot(pq, e.a)*vDot(pq, e.b) >= 0 || vDot(e.n, p)*vDot(e.n, q) >= 0 {
			continue
		}
		// 两大圆有两个对跖交点 X、-X，两侧条件只保证各含其一，
		// 还需确认是同一个交点：X 同在两弧上，或 -X 同在两弧上。
		x := vCross(pq, e.n)
		if vNorm(x) < 1e-15 {
			continue // 两大圆重合（退化，概率为零）
		}
		onPQ := vDot(vCross(p, x), pq) >= 0 && vDot(vCross(x, q), pq) >= 0
		onAB := vDot(vCross(e.a, x), e.n) >= 0 && vDot(vCross(x, e.b), e.n) >= 0
		if onPQ == onAB {
			crossings++
		}
	}
	return crossings%2 == 1
}
