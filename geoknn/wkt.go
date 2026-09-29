package geoknn

import (
	"fmt"
	"strconv"
	"strings"
)

// rawGeom 是 WKT 解析后的原始坐标结构（经纬度，度）。
// POINT/MULTIPOINT 进 points，LINESTRING/MULTILINESTRING 进 lines，
// POLYGON/MULTIPOLYGON 进 polys（每个面为环的列表，首个环为外环）。
type rawGeom struct {
	points []rawPoint
	lines  [][]rawPoint
	polys  [][][]rawPoint
}

type rawPoint [2]float64 // [经度, 纬度]

// parseWKT 解析 WKT 文本为 rawGeom。支持 POINT、LINESTRING、POLYGON
// 及对应 MULTI 类型；关键字大小写不敏感；EMPTY 视为解析失败。
func parseWKT(s string) (*rawGeom, error) {
	p := &wktParser{s: s}
	g, err := p.geometry()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("wkt: 末尾有多余内容 %q", p.s[p.i:])
	}
	return g, nil
}

type wktParser struct {
	s string
	i int
}

func (p *wktParser) skipWS() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *wktParser) expectByte(c byte) error {
	p.skipWS()
	if p.i >= len(p.s) || p.s[p.i] != c {
		return fmt.Errorf("wkt: 位置 %d 处期望 %q", p.i, string(c))
	}
	p.i++
	return nil
}

func (p *wktParser) ident() (string, error) {
	p.skipWS()
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] >= 'A' && p.s[p.i] <= 'Z' || p.s[p.i] >= 'a' && p.s[p.i] <= 'z') {
		p.i++
	}
	if p.i == start {
		return "", fmt.Errorf("wkt: 位置 %d 处期望类型关键字", start)
	}
	return strings.ToUpper(p.s[start:p.i]), nil
}

func (p *wktParser) number() (float64, error) {
	p.skipWS()
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			p.i++
		} else {
			break
		}
	}
	if p.i == start {
		return 0, fmt.Errorf("wkt: 位置 %d 处期望数字", start)
	}
	v, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		return 0, fmt.Errorf("wkt: 数字 %q 无效", p.s[start:p.i])
	}
	return v, nil
}

// coord 解析一个二维坐标 "lon lat"。
func (p *wktParser) coord() (rawPoint, error) {
	lon, err := p.number()
	if err != nil {
		return rawPoint{}, err
	}
	lat, err := p.number()
	if err != nil {
		return rawPoint{}, err
	}
	return rawPoint{lon, lat}, nil
}

// coordList 解析 "(c, c, ...)"。
func (p *wktParser) coordList() ([]rawPoint, error) {
	if err := p.expectByte('('); err != nil {
		return nil, err
	}
	var pts []rawPoint
	for {
		c, err := p.coord()
		if err != nil {
			return nil, err
		}
		pts = append(pts, c)
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		break
	}
	if err := p.expectByte(')'); err != nil {
		return nil, err
	}
	return pts, nil
}

// nestedLists 解析 "((...), (...), ...)"，每个内层是 coordList。
func (p *wktParser) nestedLists() ([][]rawPoint, error) {
	if err := p.expectByte('('); err != nil {
		return nil, err
	}
	var lists [][]rawPoint
	for {
		l, err := p.coordList()
		if err != nil {
			return nil, err
		}
		lists = append(lists, l)
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		break
	}
	if err := p.expectByte(')'); err != nil {
		return nil, err
	}
	return lists, nil
}

func (p *wktParser) geometry() (*rawGeom, error) {
	kw, err := p.ident()
	if err != nil {
		return nil, err
	}
	g := &rawGeom{}
	switch kw {
	case "POINT":
		if err := p.expectByte('('); err != nil {
			return nil, err
		}
		c, err := p.coord()
		if err != nil {
			return nil, err
		}
		if err := p.expectByte(')'); err != nil {
			return nil, err
		}
		g.points = []rawPoint{c}
	case "MULTIPOINT":
		if err := p.expectByte('('); err != nil {
			return nil, err
		}
		for {
			p.skipWS()
			if p.i < len(p.s) && p.s[p.i] == '(' {
				p.i++
				c, err := p.coord()
				if err != nil {
					return nil, err
				}
				if err := p.expectByte(')'); err != nil {
					return nil, err
				}
				g.points = append(g.points, c)
			} else {
				c, err := p.coord()
				if err != nil {
					return nil, err
				}
				g.points = append(g.points, c)
			}
			p.skipWS()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			break
		}
		if err := p.expectByte(')'); err != nil {
			return nil, err
		}
	case "LINESTRING":
		l, err := p.coordList()
		if err != nil {
			return nil, err
		}
		g.lines = [][]rawPoint{l}
	case "MULTILINESTRING":
		lists, err := p.nestedLists()
		if err != nil {
			return nil, err
		}
		g.lines = lists
	case "POLYGON":
		rings, err := p.nestedLists()
		if err != nil {
			return nil, err
		}
		g.polys = [][][]rawPoint{rings}
	case "MULTIPOLYGON":
		if err := p.expectByte('('); err != nil {
			return nil, err
		}
		for {
			rings, err := p.nestedLists()
			if err != nil {
				return nil, err
			}
			g.polys = append(g.polys, rings)
			p.skipWS()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			break
		}
		if err := p.expectByte(')'); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("wkt: 不支持的几何类型 %q", kw)
	}
	return g, nil
}
