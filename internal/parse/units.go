package parse

import (
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// UnitLink, fakülte/enstitü/MYO gibi bir birime işaret eden link.
type UnitLink struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

var (
	reSunit = regexp.MustCompile(`(?i)curSunit=(\d+)`)
	reUnit  = regexp.MustCompile(`(?i)curUnit=(\d+)`)
)

func firstID(re *regexp.Regexp, s string) int64 {
	if m := re.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseInt(m[1], 10, 64)
		return v
	}
	return 0
}

func isHeadingNode(n *html.Node) bool {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Strong, atom.B, atom.Legend, atom.Th, atom.Optgroup:
		return true
	}
	cls := strings.ToLower(attr(n, "class"))
	for _, k := range []string{"title", "heading", "header", "baslik", "unit"} {
		if strings.Contains(cls, k) {
			return true
		}
	}
	return false
}

// ParseUnitSelection, unitSelection.aspx?type=… sayfasındaki program ve birim
// linklerini belge sırasıyla toplar. Program linkinin birimi; linkteki curUnit
// parametresinden, yoksa öncesindeki son başlık/birim linkinden alınır.
func ParseUnitSelection(r io.Reader) ([]ProgramRef, []UnitLink, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, nil, err
	}
	var (
		programs    []ProgramRef
		units       []UnitLink
		heading     string
		headingUnit int64
		seenProg    = map[int64]int{}
		seenUnit    = map[int64]bool{}
	)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Head:
				return
			}
			if n.DataAtom == atom.Optgroup {
				heading, headingUnit = Clean(attr(n, "label")), 0
			}
			// Gerçek sayfa: <h5 class="panel-title"><a href="#x22">MÜHENDİSLİK FAKÜLTESİ</a>
			// <a href="facAbout.aspx?curUnit=22"><ikon/></a></h5> — ad ve ID aynı başlıkta, ayrı linklerde.
			if isHeadingNode(n) && n.DataAtom != atom.Th {
				text := Clean(textOf(n))
				if unit := firstID(reUnit, strings.Join(refsOf(n, true), " ")); unit != 0 && looksLikeUnitName(text) &&
					firstID(reSunit, strings.Join(refsOf(n, true), " ")) == 0 {
					if !seenUnit[unit] {
						seenUnit[unit] = true
						units = append(units, UnitLink{ID: unit, Name: text}) // facAbout bilgi sayfası; program listesi değil
					}
					heading, headingUnit = text, unit
					return
				}
			}
			refs := strings.Join(refsOf(n, false), " ")
			sunit, unit := firstID(reSunit, refs), firstID(reUnit, refs)
			text := Clean(textOf(n))

			if sunit != 0 {
				p := ProgramRef{ID: sunit, UnitID: unit, Name: text, UnitName: heading}
				if p.UnitID == 0 {
					p.UnitID = headingUnit
				}
				if idx, ok := seenProg[sunit]; ok {
					if programs[idx].Name == "" {
						programs[idx].Name = text
					}
				} else {
					seenProg[sunit] = len(programs)
					programs = append(programs, p)
				}
				return
			}
			if unit != 0 && text != "" {
				if !seenUnit[unit] {
					seenUnit[unit] = true
					var link string
					if ls := PageLinks([]string{refs}); len(ls) > 0 {
						link = ls[0]
					}
					units = append(units, UnitLink{ID: unit, Name: text, URL: link})
				}
				heading, headingUnit = text, unit
				return
			}
			if isHeadingNode(n) && looksLikeUnitName(text) && !hasLinkDescendant(n) {
				heading, headingUnit = text, 0
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return programs, units, nil
}

// looksLikeUnitName, gerçek sayfadaki "…artı(+) butonuna basınız" gibi yardım metinlerini eler.
// Emin olunamayan birim adı boş bırakılır; kesin ad ders planı başlığından gelir.
func looksLikeUnitName(text string) bool {
	if text == "" || len([]rune(text)) > 80 {
		return false
	}
	n := Norm(text)
	for _, w := range []string{"basiniz", "tiklayiniz", "seciniz", "listelemek", "click", "select"} {
		if strings.Contains(n, w) {
			return false
		}
	}
	return true
}

func hasLinkDescendant(n *html.Node) bool {
	found := false
	walkNodes(n, func(x *html.Node) bool {
		if x != n && x.Type == html.ElementNode && (x.DataAtom == atom.A || attr(x, "onclick") != "") {
			found = true
		}
		return !found
	})
	return found
}
