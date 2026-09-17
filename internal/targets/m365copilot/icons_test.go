package m365copilot

import "testing"

func TestParseHexColor(t *testing.T) {
	c, err := parseHexColor("#5B5FC7")
	if err != nil {
		t.Fatalf("parseHexColor() error = %v", err)
	}
	if c.R != 0x5B || c.G != 0x5F || c.B != 0xC7 || c.A != 0xFF {
		t.Errorf("parseHexColor(#5B5FC7) = %+v, want R=5B G=5F B=C7 A=FF", c)
	}

	if _, err := parseHexColor("not-a-color"); err == nil {
		t.Error("parseHexColor(not-a-color) expected error, got nil")
	}
}

func TestColorIconAndOutlineIconProducePNGs(t *testing.T) {
	color, err := colorIcon(defaultAccentColor)
	if err != nil {
		t.Fatalf("colorIcon() error = %v", err)
	}
	if len(color) < 8 || string(color[1:4]) != "PNG" {
		t.Error("colorIcon() output doesn't look like a PNG")
	}

	outline, err := outlineIcon()
	if err != nil {
		t.Fatalf("outlineIcon() error = %v", err)
	}
	if len(outline) < 8 || string(outline[1:4]) != "PNG" {
		t.Error("outlineIcon() output doesn't look like a PNG")
	}
}
