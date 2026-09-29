package geoknn

import "math"

// vec3 是单位球面上的三维单位向量（由 WGS84 经纬度转换而来）。
type vec3 [3]float64

// lonLatToVec 把经度、纬度（度）转换为三维单位向量。
// 经度 180 与 -180 自然映射到同一点；纬度 ±90 时经度不影响结果。
func lonLatToVec(lon, lat float64) vec3 {
	lonRad := lon * math.Pi / 180
	latRad := lat * math.Pi / 180
	cosLat := math.Cos(latRad)
	return vec3{
		cosLat * math.Cos(lonRad),
		cosLat * math.Sin(lonRad),
		math.Sin(latRad),
	}
}

func vAdd(a, b vec3) vec3           { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func vSub(a, b vec3) vec3           { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func vScale(a vec3, s float64) vec3 { return vec3{a[0] * s, a[1] * s, a[2] * s} }
func vDot(a, b vec3) float64        { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func vCross(a, b vec3) vec3 {
	return vec3{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func vNorm(a vec3) float64 { return math.Sqrt(vDot(a, a)) }

func vNormalize(a vec3) vec3 {
	n := vNorm(a)
	if n == 0 {
		return vec3{}
	}
	return vScale(a, 1/n)
}

// angleBetween 返回两个单位向量之间的球面夹角（弧度，[0, π]）。
// 用 atan2(|a×b|, a·b)，小角度下也有足够精度（亚毫米级）。
func angleBetween(a, b vec3) float64 {
	return math.Atan2(vNorm(vCross(a, b)), vDot(a, b))
}
