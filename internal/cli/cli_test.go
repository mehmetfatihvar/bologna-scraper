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
