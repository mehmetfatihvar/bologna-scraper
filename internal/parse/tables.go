package parse

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Cell bir tablo hücresi.
type Cell struct {
	Text    string   `json:"text"` // satır sonları korunmuş temiz metin
	Colspan int      `json:"colspan,omitempty"`
	Header  bool     `json:"header,omitempty"`
	Refs    []string `json:"refs,omitempty"` // href / onclick değerleri
}

// Row bir tablo satırı. Refs, satırın kendisi ve tüm alt elemanlarındaki linkleri içerir.
type Row struct {
	Cells []Cell   `json:"cells"`
	Refs  []string `json:"refs,omitempty"`
	Class string   `json:"class,omitempty"` // <tr class>: OİBS seçmeli havuz satırlarını "collapse_<grup>" ile işaretler
}

func (r Row) IsHeader() bool {
	if len(r.Cells) == 0 {
		return false
	}
	for _, c := range r.Cells {
		if !c.Header {
			return false
		}
	}
	return true
}

func (r Row) Texts() []string {
	out := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		out[i] = c.Text
	}
	return out
}

func (r Row) Cell(i int) string {
	if i < 0 || i >= len(r.Cells) {
		return ""
	}
	return r.Cells[i].Text
}

// Table, sayfadaki bir "yaprak" tablo (içinde başka tablo olmayan).
// Label: tablodan hemen önce gelen son metin satırı (genelde bölüm başlığı).
// Title: tablonun ilk satırı colspan'lı tek hücre ise onun metni.
type Table struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	Title string `json:"title,omitempty"`
	Rows  []Row  `json:"rows"`
}

func (t *Table) Name() string {
	if t.Title != "" {
		return t.Title
	}
	return t.Label
}

// Match, tüm kalıplar (Norm edilmiş) etiket/başlıkta geçiyorsa true döner.
func (t *Table) Match(pats ...string) bool {
	return containsAll(Norm(t.Label+" "+t.Title), pats...)
}

func (t *Table) Width() int {
	w := 0
	for _, r := range t.Rows {
		if len(r.Cells) > w {
			w = len(r.Cells)
		}
	}
	return w
}

// Text, tabloyu düz metne çevirir (bilinmeyen bölümleri kaybetmemek için).
func (t *Table) Text() string {
	var lines []string
	for _, r := range t.Rows {
		var cells []string
		for _, c := range r.Cells {
			if c.Text != "" {
				cells = append(cells, c.Text)
			}
		}
		if len(cells) > 0 {
			lines = append(lines, strings.Join(cells, " | "))
		}
	}
	return strings.Join(lines, "\n")
}

type KV struct{ Key, Value string }

// KV, anahtar|değer (veya anahtar|değer|anahtar|değer) düzenindeki tabloları okur.
func (t *Table) KV() []KV {
	var out []KV
	for _, r := range t.Rows {
		for i := 0; i+1 < len(r.Cells); i += 2 {
			k := Clean(r.Cells[i].Text)
			if k == "" {
				continue
			}
			out = append(out, KV{Key: k, Value: r.Cells[i+1].Text})
		}
	}
	return out
}

// FindHeader, ilk birkaç satır içinde verilen anahtar kelimelerden birini içeren
// satırı başlık satırı kabul eder ve Norm edilmiş hücre metinlerini döndürür.
func (t *Table) FindHeader(anyOf ...string) (int, []string) {
	for i, r := range t.Rows {
		if i > 3 {
			break
		}
		norm := make([]string, len(r.Cells))
		hit := false
		for j, c := range r.Cells {
			norm[j] = Norm(c.Text)
			for _, k := range anyOf {
				if strings.Contains(norm[j], k) {
					hit = true
				}
			}
		}
		if hit {
			return i, norm
		}
	}
	return -1, nil
}

func colIndex(headers []string, pred func(string) bool) int {
	for i, h := range headers {
		if pred(h) {
			return i
		}
	}
	return -1
}

// ParseTables HTML'i ayrıştırır ve belge sırasıyla tüm yaprak tabloları döndürür.
func ParseTables(r io.Reader) ([]Table, *html.Node, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, nil, err
	}
	w := &walker{}
	w.walk(doc)
	return w.out, doc, nil
}

type walker struct {
	buf strings.Builder
	out []Table
}

var blockAtoms = map[atom.Atom]bool{
	atom.Div: true, atom.P: true, atom.Br: true, atom.Li: true, atom.Ul: true, atom.Ol: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Tr: true, atom.Td: true, atom.Th: true, atom.Table: true, atom.Section: true,
	atom.Header: true, atom.Fieldset: true, atom.Legend: true, atom.Form: true, atom.Hr: true,
	atom.Dt: true, atom.Dd: true, atom.Label: true,
}

func (w *walker) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.buf.WriteString(n.Data)
		if w.buf.Len() > 16<<10 { // sınırsız büyümesin; sadece son satırlar önemli
			s := w.buf.String()
			if i := strings.LastIndex(s[:len(s)-2048], "\n"); i >= 0 {
				w.buf.Reset()
				w.buf.WriteString(s[i:])
			}
		}
		return
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Head, atom.Select, atom.Template:
			return
		case atom.Table:
			if !containsTable(n) {
				w.emit(n)
				return
			}
		}
	}
	blk := n.Type == html.ElementNode && blockAtoms[n.DataAtom]
	if blk {
		w.buf.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
	if blk {
		w.buf.WriteByte('\n')
	}
}

func (w *walker) emit(n *html.Node) {
	t := Table{Index: len(w.out), Label: lastLine(w.buf.String())}
	w.buf.Reset()
	walkNodes(n, func(x *html.Node) bool {
		if x.Type != html.ElementNode {
			return true
		}
		switch x.DataAtom {
		case atom.Caption:
			if s := Clean(textOf(x)); s != "" {
				t.Label = s
			}
			return false
		case atom.Tr:
			t.Rows = append(t.Rows, parseRow(x))
			return false
		}
		return true
	})
	if len(t.Rows) > 1 && len(t.Rows[0].Cells) == 1 && t.Rows[0].Cells[0].Colspan > 1 {
		t.Title = Clean(t.Rows[0].Cells[0].Text)
		t.Rows = t.Rows[1:]
	}
	w.out = append(w.out, t)
}

func parseRow(tr *html.Node) Row {
	inHead := false
	for p := tr.Parent; p != nil; p = p.Parent {
		if p.DataAtom == atom.Thead {
			inHead = true
			break
		}
		if p.DataAtom == atom.Table {
			break
		}
	}
	row := Row{Refs: refsOf(tr, true), Class: attr(tr, "class")}
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || (c.DataAtom != atom.Td && c.DataAtom != atom.Th) {
			continue
		}
		cs, _ := strconv.Atoi(attr(c, "colspan"))
		row.Cells = append(row.Cells, Cell{
			Text:    CleanLines(textOf(c)),
			Colspan: cs,
			Header:  c.DataAtom == atom.Th || inHead,
			Refs:    refsOf(c, true),
		})
	}
	return row
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := Clean(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

func containsTable(n *html.Node) bool {
	found := false
	for c := n.FirstChild; c != nil && !found; c = c.NextSibling {
		walkNodes(c, func(x *html.Node) bool {
			if x.Type == html.ElementNode && x.DataAtom == atom.Table {
				found = true
			}
			return !found
		})
	}
	return found
}

// walkNodes, n'den başlayarak derinlik öncelikli gezer; f false dönerse alt düğümlere inmez.
func walkNodes(n *html.Node, f func(*html.Node) bool) {
	if !f(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNodes(c, f)
	}
}

// textOf, blok elemanlarda satır sonu koyarak düğümün metnini toplar.
func textOf(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(x *html.Node) {
		switch x.Type {
		case html.TextNode:
			b.WriteString(x.Data)
			return
		case html.ElementNode:
			if x.DataAtom == atom.Script || x.DataAtom == atom.Style {
				return
			}
		}
		blk := x.Type == html.ElementNode && blockAtoms[x.DataAtom]
		if blk {
			b.WriteByte('\n')
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
		if blk {
			b.WriteByte('\n')
		}
	}
	rec(n)
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

var refAttrs = []string{"href", "onclick", "data-url", "data-href", "value"}

// refsOf, düğümün (ve istenirse alt düğümlerinin) link benzeri özniteliklerini toplar.
func refsOf(n *html.Node, deep bool) []string {
	var out []string
	add := func(x *html.Node) {
		for _, k := range refAttrs {
			v := strings.TrimSpace(attr(x, k))
			if v == "" || v == "#" || strings.HasPrefix(v, "javascript:void") {
				continue
			}
			if k == "value" && !strings.Contains(v, "=") { // input value'ları gürültü
				continue
			}
			out = append(out, v)
		}
	}
	if !deep {
		add(n)
		return out
	}
	walkNodes(n, func(x *html.Node) bool {
		if x.Type == html.ElementNode {
			add(x)
		}
		return true
	})
	return out
}

// Debug, tabloların kısa bir dökümünü üretir (inspect komutu için).
func Debug(ts []Table, maxRows int) string {
	var b strings.Builder
	for _, t := range ts {
		fmt.Fprintf(&b, "── #%d  label=%q", t.Index, t.Label)
		if t.Title != "" {
			fmt.Fprintf(&b, "  title=%q", t.Title)
		}
		fmt.Fprintf(&b, "  (%d satır × %d sütun)\n", len(t.Rows), t.Width())
		for i, r := range t.Rows {
			if i >= maxRows {
				fmt.Fprintf(&b, "   … %d satır daha\n", len(t.Rows)-maxRows)
				break
			}
			cells := r.Texts()
			for j, c := range cells {
				c = strings.ReplaceAll(c, "\n", " ⏎ ")
				if len([]rune(c)) > 60 {
					c = string([]rune(c)[:60]) + "…"
				}
				cells[j] = c
			}
			hdr := ""
			if r.IsHeader() {
				hdr = "[TH] "
			}
			fmt.Fprintf(&b, "   %s%s\n", hdr, strings.Join(cells, " │ "))
		}
	}
	return b.String()
}
