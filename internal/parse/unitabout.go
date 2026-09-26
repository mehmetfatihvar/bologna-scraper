package parse

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// UnitAbout, birim (fakülte / yüksekokul / enstitü) tanıtım sayfası: facAbout.aspx?curUnit=…
type UnitAbout struct {
	Name  string `json:"name,omitempty"`  // "MÜHENDİSLİK FAKÜLTESİ Bilgi Paketi Tanımları" başlığından
	About string `json:"about,omitempty"` // tanıtım metni; paragraflar satır sonuyla ayrılır
}

const unitAboutSuffix = "Bilgi Paketi Tanımları"

// ParseUnitAbout, facAbout.aspx sayfasını ayrıştırır. İçerik tablo değil, lblContent içinde serbest HTML'dir.
// Tanımsız birimde başlık "Fakülte Bilgi Paketi Tanımları", içerik boştur.
func ParseUnitAbout(body []byte) (*UnitAbout, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	u := &UnitAbout{}
	walkNodes(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.DataAtom != atom.Span {
			return true
		}
		switch strings.ToLower(attr(n, "id")) {
		case "lblheader":
			h := Clean(textOf(n))
			if name, ok := strings.CutSuffix(h, unitAboutSuffix); ok {
				h = strings.TrimSpace(name)
			}
			if Norm(h) != "fakulte" { // yer tutucu başlık: birim tanımsız
				u.Name = h
			}
			return false
		case "lblcontent":
			u.About = CleanLines(textOf(n))
			return false
		}
		return true
	})
	return u, nil
}
