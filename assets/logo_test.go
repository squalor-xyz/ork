// SPDX-License-Identifier: GPL-3.0-or-later

// Package assets tests the ork logo files, logo.svg and logo-square.svg,
// and the README header that shows them. The package has no code of its
// own. The tests read the files by their paths relative to this directory.
//
// The SVG checks parse each file as XML and follow fill and stroke through
// the element tree: an element's paint is its own attribute, or else its
// nearest ancestor's. They do not render the file. What they check, and
// what they do not, is listed on each check. Paths are not measured.
package assets

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	svgNS       = "http://www.w3.org/2000/svg"
	tealColour  = "#6ee7d2"
	creamColour = "#f4efe7"
)

// sliceColours are the blue, violet and orange of the three slices.
var sliceColours = []string{"#3b82f6", "#8b5cf6", "#ea580c"}

// allowedElements are the only elements a logo may use. Script, use,
// image, style, foreignObject, animate, set, mask and the rest are not in
// the list, so they cannot change paint or add content the checks miss.
var allowedElements = map[string]bool{
	"svg": true, "title": true, "desc": true, "g": true,
	"path": true, "rect": true, "circle": true,
}

// logoFiles lists the SVG files and the viewBox each one must declare.
var logoFiles = []struct {
	path          string
	width, height float64
}{
	{"logo.svg", 200, 144},
	{"logo-square.svg", 64, 64},
}

// element is one start tag of an SVG file. fill and stroke are the paint
// the element gets: its own attribute, else its nearest ancestor's, else ""
// (unset). Unset stroke means none. Unset fill on a shape means black.
type element struct {
	ns     string
	name   string
	depth  int // 1 for the root element
	attrs  map[string]string
	fill   string
	stroke string
	text   string // character data directly inside the element
}

// box is an axis-aligned box in viewBox units. x1 and y1 are the far edges.
type box struct{ x0, y0, x1, y1 float64 }

func (b box) String() string {
	return fmt.Sprintf("[%g %g %g %g]", b.x0, b.y0, b.x1, b.y1)
}

// inside reports whether b lies within o, edges included.
func (b box) inside(o box) bool {
	return b.x0 >= o.x0 && b.y0 >= o.y0 && b.x1 <= o.x1 && b.y1 <= o.y1
}

func isSlice(colour string) bool {
	for _, c := range sliceColours {
		if c == colour {
			return true
		}
	}
	return false
}

func allowedPaint(v string) bool {
	return v == "none" || v == tealColour || v == creamColour || isSlice(v)
}

func isShape(name string) bool {
	return name == "rect" || name == "circle" || name == "path"
}

// parseSVG reads the file as XML and returns its elements in document
// order. The root is first, at depth 1.
func parseSVG(data []byte) ([]element, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var elems []element
	var open []int // index in elems of each open element
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return elems, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			e := element{
				ns:    t.Name.Space,
				name:  t.Name.Local,
				depth: len(open) + 1,
				attrs: make(map[string]string, len(t.Attr)),
			}
			for _, a := range t.Attr {
				e.attrs[a.Name.Local] = a.Value
			}
			if len(open) > 0 {
				parent := elems[open[len(open)-1]]
				e.fill, e.stroke = parent.fill, parent.stroke
			}
			if v, ok := e.attrs["fill"]; ok {
				e.fill = v
			}
			if v, ok := e.attrs["stroke"]; ok {
				e.stroke = v
			}
			elems = append(elems, e)
			open = append(open, len(elems)-1)
		case xml.EndElement:
			open = open[:len(open)-1]
		case xml.CharData:
			if len(open) > 0 {
				elems[open[len(open)-1]].text += string(t)
			}
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// loadSVG reads one logo file and parses it. A read or XML error fails the
// test.
func loadSVG(t *testing.T, path string) ([]byte, []element) {
	t.Helper()
	data := readFile(t, path)
	elems, err := parseSVG(data)
	if err != nil {
		t.Fatalf("%s: not well-formed XML: %v", path, err)
	}
	return data, elems
}

func TestSVGFilesAreSVGWithViewBoxTitleAndDesc(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			_, elems := loadSVG(t, f.path)
			for _, p := range checkRoot(f.path, elems, f.width, f.height) {
				t.Error(p)
			}
		})
	}
}

// checkRoot checks that the root is an svg in the SVG namespace with the
// expected viewBox, and that it has a non-empty title and desc as direct
// children.
func checkRoot(name string, elems []element, w, h float64) []string {
	if len(elems) == 0 {
		return []string{name + ": no elements"}
	}
	var problems []string
	root := elems[0]
	if root.name != "svg" || root.ns != svgNS {
		problems = append(problems, fmt.Sprintf("%s: root must be <svg> with xmlns=%q, got <%s> in namespace %q", name, svgNS, root.name, root.ns))
	}
	if !viewBoxIs(root.attrs["viewBox"], w, h) {
		problems = append(problems, fmt.Sprintf("%s: viewBox must be \"0 0 %g %g\", got %q", name, w, h, root.attrs["viewBox"]))
	}
	for _, want := range []string{"title", "desc"} {
		if !hasText(elems, want) {
			problems = append(problems, fmt.Sprintf("%s: root needs a non-empty <%s> child", name, want))
		}
	}
	return problems
}

func viewBoxIs(v string, w, h float64) bool {
	f := strings.Fields(v)
	if len(f) != 4 {
		return false
	}
	nums := make([]float64, 4)
	for i, s := range f {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return false
		}
		nums[i] = n
	}
	return nums[0] == 0 && nums[1] == 0 && nums[2] == w && nums[3] == h
}

// hasText reports whether a direct child of the root named name has
// non-blank text.
func hasText(elems []element, name string) bool {
	for _, e := range elems {
		if e.depth == 2 && e.name == name && strings.TrimSpace(e.text) != "" {
			return true
		}
	}
	return false
}

func TestSVGFilesHaveNoActiveContentOrRemoteURLs(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			data, elems := loadSVG(t, f.path)
			for _, p := range checkElements(f.path, elems) {
				t.Error(p)
			}
			for _, p := range checkURLs(f.path, data) {
				t.Error(p)
			}
		})
	}
}

// checkElements rejects any element that is not in allowedElements (this
// covers script, foreignObject, image, use and style), a nested svg, an
// element outside the SVG namespace, and the attributes that add a link,
// a style, a transform or a url(...) reference. Any of these could change
// what the checks read.
func checkElements(name string, elems []element) []string {
	var problems []string
	for i, e := range elems {
		if e.ns != svgNS || !allowedElements[e.name] || (e.name == "svg" && i != 0) {
			problems = append(problems, fmt.Sprintf("%s: element <%s> (namespace %q) is not allowed", name, e.name, e.ns))
		}
		for _, attr := range []string{"href", "style", "transform"} {
			if _, ok := e.attrs[attr]; ok {
				problems = append(problems, fmt.Sprintf("%s: <%s> has a %s attribute, which is not allowed", name, e.name, attr))
			}
		}
		for attr, v := range e.attrs {
			if strings.Contains(v, "url(") {
				problems = append(problems, fmt.Sprintf("%s: <%s> %s=%q refers to a url(), which is not allowed", name, e.name, attr, v))
			}
		}
	}
	return problems
}

// checkURLs rejects any http or https URL in the file except the SVG
// namespace declaration. It searches the raw bytes, so comments count too.
func checkURLs(name string, data []byte) []string {
	lower := strings.ToLower(string(data))
	rest := strings.ReplaceAll(lower, `xmlns="`+svgNS+`"`, "")
	if strings.Contains(rest, "http://") || strings.Contains(rest, "https://") {
		return []string{name + ": contains an http or https URL other than the SVG xmlns"}
	}
	return nil
}

func TestSVGFilesPaintOnlyTheLogoColours(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			_, elems := loadSVG(t, f.path)
			for _, p := range checkPaint(f.path, elems, f.width, f.height) {
				t.Error(p)
			}
		})
	}
}

// checkPaint checks colours and the background.
//
// Colours: every fill and stroke attribute must be teal, cream, one of the
// three slice colours or none. A shape with no fill, its own or inherited,
// would render black, so it fails.
//
// Background: a rect or circle whose fill is not none must not cover the
// whole viewBox. Rects are measured exactly. Circles are measured by their
// four viewBox corners. Paths are not measured, so a path background is not
// caught.
func checkPaint(name string, elems []element, w, h float64) []string {
	var problems []string
	for _, e := range elems {
		for _, attr := range []string{"fill", "stroke"} {
			if v, ok := e.attrs[attr]; ok && !allowedPaint(v) {
				problems = append(problems, fmt.Sprintf("%s: <%s> %s=%q is not an allowed logo colour", name, e.name, attr, v))
			}
		}
		if !isShape(e.name) {
			continue
		}
		if e.fill == "" {
			problems = append(problems, fmt.Sprintf("%s: <%s> has no fill and would render black", name, e.name))
			continue
		}
		if e.fill == "none" {
			continue
		}
		covers, err := coversViewBox(e, w, h)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: <%s>: %v", name, e.name, err))
		} else if covers {
			problems = append(problems, fmt.Sprintf("%s: <%s> covers the whole viewBox (a background)", name, e.name))
		}
	}
	return problems
}

// coversViewBox reports whether a rect or circle covers the whole viewBox.
// A path returns false because its geometry is not measured.
func coversViewBox(e element, w, h float64) (bool, error) {
	switch e.name {
	case "rect":
		b, err := bounds(e, w, h)
		if err != nil {
			return false, err
		}
		return b.x0 <= 0 && b.y0 <= 0 && b.x1 >= w && b.y1 >= h, nil
	case "circle":
		cx, cy, r, err := circleGeom(e, w, h)
		if err != nil {
			return false, err
		}
		for _, c := range [][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
			dx, dy := c[0]-cx, c[1]-cy
			if dx*dx+dy*dy > r*r {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func TestSVGCreamShapesSitInsideSliceEdges(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			_, elems := loadSVG(t, f.path)
			for _, p := range checkCreamEdges(f.path, elems, f.width, f.height) {
				t.Error(p)
			}
		})
	}
}

// checkCreamEdges is the slice-edge rule. Every rect or circle with a cream
// fill must lie inside a slice-coloured rect, so no cream shape stands alone
// on a light page. A circle is judged by its bounding box, which is stricter
// than the circle. Only rects count as containers. A cream path cannot be
// placed without geometry, so it fails.
func checkCreamEdges(name string, elems []element, w, h float64) []string {
	var problems []string
	var edges []box
	for _, e := range elems {
		if e.name == "rect" && isSlice(e.fill) {
			b, err := bounds(e, w, h)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: slice <rect>: %v", name, err))
				continue
			}
			edges = append(edges, b)
		}
	}
	for _, e := range elems {
		if !isShape(e.name) || e.fill != creamColour {
			continue
		}
		if e.name == "path" {
			problems = append(problems, name+": cream <path> cannot be checked against a slice edge")
			continue
		}
		b, err := bounds(e, w, h)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: cream <%s>: %v", name, e.name, err))
			continue
		}
		if !insideAny(b, edges) {
			problems = append(problems, fmt.Sprintf("%s: cream <%s> %v lies outside every slice-coloured rect", name, e.name, b))
		}
	}
	return problems
}

func insideAny(b box, edges []box) bool {
	for _, o := range edges {
		if b.inside(o) {
			return true
		}
	}
	return false
}

// length reads a coordinate or size. "100%" means the viewBox size given
// as full. Other units are rejected by ParseFloat.
func length(attrs map[string]string, key string, def, full float64) (float64, error) {
	v, ok := attrs[key]
	if !ok {
		return def, nil
	}
	if v == "100%" {
		return full, nil
	}
	return strconv.ParseFloat(v, 64)
}

// bounds returns the box of a rect or circle.
func bounds(e element, w, h float64) (box, error) {
	switch e.name {
	case "rect":
		x, err := length(e.attrs, "x", 0, w)
		if err != nil {
			return box{}, fmt.Errorf("cannot read x: %v", err)
		}
		y, err := length(e.attrs, "y", 0, h)
		if err != nil {
			return box{}, fmt.Errorf("cannot read y: %v", err)
		}
		rw, err := length(e.attrs, "width", 0, w)
		if err != nil {
			return box{}, fmt.Errorf("cannot read width: %v", err)
		}
		rh, err := length(e.attrs, "height", 0, h)
		if err != nil {
			return box{}, fmt.Errorf("cannot read height: %v", err)
		}
		return box{x, y, x + rw, y + rh}, nil
	case "circle":
		cx, cy, r, err := circleGeom(e, w, h)
		if err != nil {
			return box{}, err
		}
		return box{cx - r, cy - r, cx + r, cy + r}, nil
	}
	return box{}, fmt.Errorf("no bounds for <%s>", e.name)
}

// circleGeom reads a circle's centre and radius. The radius must be a plain
// number.
func circleGeom(e element, w, h float64) (cx, cy, r float64, err error) {
	cx, err = length(e.attrs, "cx", 0, w)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cannot read cx: %v", err)
	}
	cy, err = length(e.attrs, "cy", 0, h)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cannot read cy: %v", err)
	}
	r, err = strconv.ParseFloat(e.attrs["r"], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cannot read r: %v", err)
	}
	return cx, cy, r, nil
}

func TestSVGFilesShareTheSliceColoursAndTeal(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			_, elems := loadSVG(t, f.path)
			fills, strokes := paintedColours(elems)
			// Each file must fill shapes with all three slice colours. Only
			// these three are on the colour whitelist (see
			// TestSVGFilesPaintOnlyTheLogoColours), so both files then use
			// exactly the same three.
			for _, c := range sliceColours {
				if !fills[c] {
					t.Errorf("%s: no shape is filled %s, one of the three slice colours", f.path, c)
				}
			}
			if !fills[tealColour] && !strokes[tealColour] {
				t.Errorf("%s: no shape uses the teal %s", f.path, tealColour)
			}
		})
	}
}

// paintedColours returns the fill and stroke colours that paint a shape,
// using each shape's effective paint.
func paintedColours(elems []element) (fills, strokes map[string]bool) {
	fills, strokes = map[string]bool{}, map[string]bool{}
	for _, e := range elems {
		if isShape(e.name) {
			fills[e.fill] = true
			strokes[e.stroke] = true
		}
	}
	return fills, strokes
}

func TestSVGFilesHaveNoCopyrightText(t *testing.T) {
	for _, f := range logoFiles {
		t.Run(f.path, func(t *testing.T) {
			lower := strings.ToLower(string(readFile(t, f.path)))
			for _, s := range []string{"copyright", "(c)"} {
				if strings.Contains(lower, s) {
					t.Errorf("%s: contains %q; the logo carries no copyright text beyond the repository licence", f.path, s)
				}
			}
		})
	}
}

func TestREADMEStartsWithTheLogo(t *testing.T) {
	for _, p := range checkREADMEHeader(readFile(t, "../README.md")) {
		t.Error(p)
	}
}

// checkREADMEHeader checks that the first non-blank line is an img tag for
// assets/logo.svg, with a non-empty alt that starts with "ork" and a width,
// and that the next non-blank line is the "# ork" heading.
func checkREADMEHeader(data []byte) []string {
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) < 2 {
		return []string{"README.md: needs a logo line and a # ork heading"}
	}
	attrs, err := imgAttrs(lines[0])
	if err != nil {
		return []string{fmt.Sprintf("README.md: first non-blank line must be an <img> tag, got %q: %v", lines[0], err)}
	}
	var problems []string
	if attrs["src"] != "assets/logo.svg" {
		problems = append(problems, fmt.Sprintf("README.md: <img> src must be assets/logo.svg, got %q", attrs["src"]))
	}
	if !strings.HasPrefix(attrs["alt"], "ork") {
		problems = append(problems, fmt.Sprintf("README.md: <img> alt must start with \"ork\", got %q", attrs["alt"]))
	}
	if attrs["width"] == "" {
		problems = append(problems, "README.md: <img> needs a non-empty width attribute")
	}
	if lines[1] != "# ork" {
		problems = append(problems, fmt.Sprintf("README.md: the line after the logo must be the \"# ork\" heading, got %q", lines[1]))
	}
	return problems
}

// imgAttrs parses one HTML img tag into its attributes. The tag is read as
// XML, so entities are decoded. A trailing "/>" is accepted.
func imgAttrs(tag string) (map[string]string, error) {
	if strings.HasSuffix(tag, "/>") {
		tag = strings.TrimSuffix(tag, "/>") + ">"
	}
	dec := xml.NewDecoder(strings.NewReader(tag + "</img>"))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	start, ok := tok.(xml.StartElement)
	if !ok || start.Name.Local != "img" {
		return nil, fmt.Errorf("not an img start tag")
	}
	attrs := make(map[string]string, len(start.Attr))
	for _, a := range start.Attr {
		attrs[a.Name.Local] = a.Value
	}
	return attrs, nil
}
