package profile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// point is one curve point: Temp °C, Pct %.
type point struct{ Temp, Pct int }

// pwmPct converts a raw 0..255 duty to percent.
func pwmPct(pwm int) int { return clampInt((pwm*100+127)/255, 0, 100) }

func clampInt(n, lo, hi int) int { return min(max(n, lo), hi) }

// curveText makes the curve text our settings take: temperatures 0..120 rising without repeats, speeds 0..100, 2 to 8
// points. A single point is widened to a flat line; more than 8 points are thinned evenly. what says which curve it is
// for the warnings.
func (r *Result) curveText(what string, pts []point) string {
	for i := range pts {
		pts[i].Temp, pts[i].Pct = clampInt(pts[i].Temp, 0, 120), clampInt(pts[i].Pct, 0, 100)
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Temp < pts[j].Temp })
	var uniq []point
	for _, p := range pts {
		if n := len(uniq); n > 0 && uniq[n-1].Temp == p.Temp {
			uniq[n-1] = p
			r.warnf("%s: two points at %d °C, the later one is used", what, p.Temp)
			continue
		}
		uniq = append(uniq, p)
	}
	if len(uniq) == 1 {
		p := uniq[0]
		if p.Temp+10 <= 120 {
			uniq = append(uniq, point{p.Temp + 10, p.Pct})
		} else {
			uniq = []point{{p.Temp - 10, p.Pct}, p}
		}
		r.warnf("%s: a single point is a flat line, widened to two points", what)
	}
	if len(uniq) > 8 {
		thin := make([]point, 8)
		for i := range thin {
			thin[i] = uniq[(i*(len(uniq)-1)+3)/7]
		}
		r.warnf("%s: %d points do not fit, thinned to 8", what, len(uniq))
		uniq = thin
	}
	parts := make([]string, len(uniq))
	for i, p := range uniq {
		parts[i] = fmt.Sprintf("%d:%d", p.Temp, p.Pct)
	}
	return strings.Join(parts, ",")
}

// parseCurvePairs reads "temp:value,temp:value" with decimals allowed; pairs without ":" or numbers are reported and
// skipped. convert turns the value into percent.
func (r *Result) parseCurvePairs(what, text string, convert func(float64) int) []point {
	var pts []point
	for _, part := range strings.Split(text, ",") {
		part = strings.ReplaceAll(part, " ", "")
		if part == "" {
			continue
		}
		t, v, ok := strings.Cut(part, ":")
		tf, err1 := strconv.ParseFloat(t, 64)
		vf, err2 := strconv.ParseFloat(v, 64)
		if !ok || err1 != nil || err2 != nil {
			r.warnf("%s: the pair %q is not temp:value, skipped", what, clipPair(part))
			continue
		}
		pts = append(pts, point{int(tf + 0.5), convert(vf)})
	}
	return pts
}

func clipPair(s string) string {
	if len(s) > 24 {
		return s[:24]
	}
	return s
}

func clipN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
