package cli

import "testing"

func TestParseProgramArg(t *testing.T) {
	r, err := parseProgramArg("352")
	if err != nil || r.ID != 352 || r.UnitID != 0 {
		t.Fatal(r, err)
	}
	r, err = parseProgramArg("https://obs.mersin.edu.tr/oibs/bologna/index.aspx?lang=tr&curOp=showPac&curUnit=12&curSunit=352#")
	if err != nil || r.ID != 352 || r.UnitID != 12 {
		t.Fatal(r, err)
	}
	if _, err := parseProgramArg("abc"); err == nil {
		t.Fatal("hata bekleniyordu")
	}
}

func TestLevelList(t *testing.T) {
	if _, err := levelList([]string{"lisans", "yanlis"}, false); err == nil {
		t.Fatal("bilinmeyen seviye kabul edildi")
	}
	if l, _ := levelList(nil, true); len(l) != 4 {
		t.Fatal(l)
	}
}

func TestParseYears(t *testing.T) {
	for in, want := range map[string]int{"1": 1, "3": 3, "all": -1, "ALL": -1} {
		if got, err := parseYears(in); err != nil || got != want {
			t.Fatalf("%q -> %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"0", "-2", "x", ""} {
		if _, err := parseYears(bad); err == nil {
			t.Fatalf("%q kabul edildi", bad)
		}
	}
}
