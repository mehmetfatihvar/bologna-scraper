package parse

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// YearField, ders planı sayfasındaki yıl seçicisinin form alanı adı.
const YearField = "cmbYillar"

// Form, bir ASP.NET sayfasının ilk formu: gönderim adresi ve (tarayıcının göndereceği) alan değerleri.
type Form struct {
	Action string
	Fields url.Values
}

// ParseForm, sayfadaki ilk <form>'u okur: gizli alanlar (__VIEWSTATE, __EVENTVALIDATION…), metin
// alanları ve seçili <select> değerleri. Postback'i tarayıcı gibi yapmak için kullanılır.
func ParseForm(body []byte) (*Form, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	f := &Form{Fields: url.Values{}}
	var form *html.Node
	walkNodes(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Form {
			form = n
			return false
		}
		return form == nil
	})
	if form == nil {
		return f, nil
	}
	f.Action = html.UnescapeString(attr(form, "action"))
	walkNodes(form, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		name := attr(n, "name")
		switch n.DataAtom {
		case atom.Input:
			switch strings.ToLower(attr(n, "type")) {
			case "submit", "button", "image", "reset", "file":
			case "checkbox", "radio":
				if name != "" && hasAttr(n, "checked") {
					f.Fields.Add(name, attrOr(n, "value", "on"))
				}
			default:
				if name != "" {
					f.Fields.Add(name, attr(n, "value"))
				}
			}
		case atom.Select:
			if name == "" {
				return false
			}
			first, chosen := "", ""
			found := false
			for o := n.FirstChild; o != nil; o = o.NextSibling {
				if o.Type != html.ElementNode || o.DataAtom != atom.Option {
					continue
				}
				v := attrOr(o, "value", Clean(textOf(o)))
				if first == "" {
					first = v
				}
				if hasAttr(o, "selected") && !found {
					chosen, found = v, true
				}
			}
			if !found {
				chosen = first
			}
			f.Fields.Set(name, chosen)
			return false
		case atom.Textarea:
			if name != "" {
				f.Fields.Add(name, textOf(n))
			}
			return false
		}
		return true
	})
	return f, nil
}

// attrOr, öznitelik yoksa def döndürür (value="" ile hiç olmamasını ayırt eder).
func attrOr(n *html.Node, key, def string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return def
}

// hasAttr, boolean öznitelikler (<option selected>, <input checked>) için.
func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}
