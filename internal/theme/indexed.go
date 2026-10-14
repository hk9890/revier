package theme

import (
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

// In is the theme as a terminal with profile p shows it. A terminal with 256
// colours or 16 gets the palette index revier has always drawn each colour
// with, chosen as termenv chose it. The renderer's own choice is darker for
// the greys, and black for the hover row in 16 colours, where a dark terminal
// does not show it.
func (t Theme) In(p colorprofile.Profile) Theme {
	in := fromFlavor(t.Name, flavors[t.Name], t.Glyphs, p)
	in.Spinner = t.Spinner
	return in
}

// inProfile is a palette colour for a terminal with profile p. An index passes
// the renderer unchanged; a hex colour is converted by it.
func inProfile(hex string, p colorprofile.Profile) color.Color {
	switch p {
	case colorprofile.ANSI256:
		return indexed(hex)
	case colorprofile.ANSI:
		return basic(indexed(hex))
	}
	return lipgloss.Color(hex)
}

// indexed is the 256-colour index termenv gives a hex colour: the nearer of
// the nearest cube colour and a grey. Its grey is taken from the average of
// the cube coordinates, not of the colour, so it is almost always the darkest.
// That is a termenv bug, kept because it is what revier drew.
func indexed(hex string) ansi.IndexedColor {
	c, _ := colorful.Hex(hex)
	cube := func(v float64) int {
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		}
		return int((v - 35) / 40)
	}
	r, g, b := cube(c.R*255), cube(c.G*255), cube(c.B*255)
	level := [6]float64{0, 0x5f, 0x87, 0xaf, 0xd7, 0xff}
	nearCube := colorful.Color{R: level[r] / 255, G: level[g] / 255, B: level[b] / 255}

	grey := 23
	if average := (r + g + b) / 3; average <= 238 {
		grey = (average - 3) / 10
	}
	gv := float64(8+10*grey) / 255
	nearGrey := colorful.Color{R: gv, G: gv, B: gv}

	if c.DistanceHSLuv(nearCube) <= c.DistanceHSLuv(nearGrey) {
		return ansi.IndexedColor(16 + 36*r + 6*g + b)
	}
	return ansi.IndexedColor(232 + grey)
}

// basic is the one of the 16 colours termenv gives a 256-colour index: the
// nearest by HSLuv distance.
func basic(i ansi.IndexedColor) ansi.BasicColor {
	c, _ := colorful.MakeColor(i)
	var nearest ansi.BasicColor
	least := math.MaxFloat64
	for b := range ansi.BasicColor(16) {
		bc, _ := colorful.MakeColor(ansi.IndexedColor(b))
		if d := c.DistanceHSLuv(bc); d < least {
			nearest, least = b, d
		}
	}
	return nearest
}
