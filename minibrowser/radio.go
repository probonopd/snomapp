package minibrowser

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tuneinRoot   = "http://opml.radiotime.com/"
	tuneinSearch = "http://opml.radiotime.com/Search.ashx"
)

var tuneinClient = &http.Client{Timeout: 10 * time.Second}

// ---- TuneIn OPML types -----------------------------------------------------

// opmlDoc is the root element of a TuneIn radiotime OPML response.
type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Head    struct {
		Title string `xml:"title"`
	} `xml:"head"`
	Body struct {
		Outlines []opmlOutline `xml:"outline"`
	} `xml:"body"`
}

// opmlOutline is a single entry in a TuneIn OPML document.
// type="link"  – navigation item; URL points to another OPML page.
// type="audio" – playable station; URL points to Tune.ashx which resolves the stream.
// type="text"  – display-only group header; may contain child outlines.
type opmlOutline struct {
	Type     string        `xml:"type,attr"`
	Text     string        `xml:"text,attr"`
	URL      string        `xml:"URL,attr"`
	Key      string        `xml:"key,attr"`
	SubText  string        `xml:"subtext,attr"`
	BitRate  string        `xml:"bitrate,attr"`
	Children []opmlOutline `xml:"outline"`
}

// fetchOPML downloads and parses a TuneIn OPML document from rawURL.
func fetchOPML(rawURL string) (*opmlDoc, error) {
	resp, err := tuneinClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var doc opmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("opml parse: %w", err)
	}
	return &doc, nil
}

// ---- handlers --------------------------------------------------------------

// RadioMenu serves the TuneIn top-level category browse menu.
func (h *Handler) RadioMenu(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	doc, err := fetchOPML(tuneinRoot)
	if err != nil {
		log.Printf("tunein root: %v", err)
		h.writeText(w, r, "Radio", "TuneIn unavailable.\nCheck network connectivity.")
		return
	}

	menu := PhoneMenu{Title: "Radio"}
	for _, o := range doc.Body.Outlines {
		if o.URL == "" {
			continue
		}
		text := o.Text
		if text == "" {
			text = o.Key
		}
		thisBack := b + "/radio"
		menu.MenuItems = append(menu.MenuItems, MenuItem{
			Name: text,
			URL:  navURL(b + "/radio/browse?url=" + url.QueryEscape(o.URL) + "&back=" + url.QueryEscape(thisBack)),
		})
	}
	menu.MenuItems = append(menu.MenuItems,
		MenuItem{Name: "Search", URL: navURL(b + "/radio/search")},
		MenuItem{Name: "< Back", URL: actionURL(b + "/")},
	)
	menu.SoftKeys = oneLevelBackKeys(b + "/")
	h.writeXML(w, menu)
}

// RadioBrowse fetches and renders any TuneIn OPML URL as a navigable menu.
// Query parameters:
//
//	url  – the OPML URL to fetch (required)
//	back – URL to navigate to when the user presses Back
func (h *Handler) RadioBrowse(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	opmlURL := r.URL.Query().Get("url")
	if opmlURL == "" {
		h.RadioMenu(w, r)
		return
	}

	backURL := r.URL.Query().Get("back")
	if backURL == "" {
		backURL = b + "/radio"
	}

	doc, err := fetchOPML(opmlURL)
	if err != nil {
		log.Printf("tunein browse %s: %v", opmlURL, err)
		h.writeText(w, r, "Radio", "Failed to load.\nCheck network connectivity.")
		return
	}

	title := doc.Head.Title
	if title == "" {
		title = "Radio"
	}

	// thisPageURL is used as the "back" value when linking to sub-pages from here.
	thisPageURL := b + "/radio/browse?url=" + url.QueryEscape(opmlURL) + "&back=" + url.QueryEscape(backURL)

	menu := PhoneMenu{Title: title}
	for _, o := range doc.Body.Outlines {
		radioAppendOutline(&menu, b, o, thisPageURL)
	}
	menu.MenuItems = append(menu.MenuItems, MenuItem{Name: "< Back", URL: actionURL(backURL)})
	menu.SoftKeys = oneLevelBackKeys(backURL)
	h.writeXML(w, menu)
}

// radioAppendOutline converts one TuneIn OPML outline into menu item(s) and
// appends them to menu. backURL is set as the "back" target for any
// deeper navigation items generated from this outline.
func radioAppendOutline(menu *PhoneMenu, b string, o opmlOutline, backURL string) {
	// TuneIn often wraps groups of items in a container outline that has no
	// type attribute and no URL (e.g. <outline text="Stations" key="stations">).
	// Always recurse into children first so nothing is silently dropped.
	if len(o.Children) > 0 {
		for _, child := range o.Children {
			radioAppendOutline(menu, b, child, backURL)
		}
		return
	}

	switch o.Type {
	case "audio":
		menu.MenuItems = append(menu.MenuItems, MenuItem{
			Name: radioStationLabel(o),
			URL: navURL(b + "/radio/tune?url=" + url.QueryEscape(o.URL) +
				"&name=" + url.QueryEscape(o.Text) +
				"&back=" + url.QueryEscape(backURL)),
		})
	default:
		// "link", empty type, or anything else with a URL → browse link.
		if o.URL != "" {
			menu.MenuItems = append(menu.MenuItems, MenuItem{
				Name: o.Text,
				URL:  navURL(b + "/radio/browse?url=" + url.QueryEscape(o.URL) + "&back=" + url.QueryEscape(backURL)),
			})
		}
	}
}

// radioStationLabel formats a concise display label for an audio outline.
func radioStationLabel(o opmlOutline) string {
	if o.SubText != "" {
		return truncate(o.Text, 18) + " · " + truncate(o.SubText, 16)
	}
	return o.Text
}

// RadioSearch serves a search input form (no ?q= param) or search results (?q=<term>).
func (h *Handler) RadioSearch(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	if q == "" {
		// Show input form.
		inputToken := "__QUERY__"
		h.writeXML(w, PhoneInput{
			Track: "no",
			URL:   InputURL{Value: b + "/radio/search?q=" + inputToken},
			InputItem: InputItem{
				DisplayName: "Search Radio",
				InputToken:  inputToken,
				InputFlags:  "a",
			},
			SoftKeys: oneLevelBackKeys(b + "/radio"),
		})
		return
	}

	searchURL := tuneinSearch + "?query=" + url.QueryEscape(q)
	backURL := b + "/radio/search" // back to blank search form
	thisPageURL := b + "/radio/search?q=" + url.QueryEscape(q)

	doc, err := fetchOPML(searchURL)
	if err != nil {
		log.Printf("tunein search %q: %v", q, err)
		h.writeText(w, r, "Radio Search", "Search failed.\nCheck network connectivity.")
		return
	}

	menu := PhoneMenu{Title: "Results: " + q}
	for _, o := range doc.Body.Outlines {
		radioAppendOutline(&menu, b, o, thisPageURL)
	}
	if len(menu.MenuItems) == 0 {
		h.writeText(w, r, "Radio Search", "No results for:\n"+q)
		return
	}
	menu.MenuItems = append(menu.MenuItems, MenuItem{Name: "< Back", URL: actionURL(backURL)})
	menu.SoftKeys = oneLevelBackKeys(backURL)
	h.writeXML(w, menu)
}

// RadioTune fetches a TuneIn station's stream URL(s) and displays them.
// TuneIn's Tune.ashx endpoint returns a plain-text list of stream URLs,
// a PLS file, an M3U file, or occasionally an OPML document.
//
// Query parameters:
//
//	url  – the TuneIn Tune.ashx URL (required)
//	name – station display name
//	back – URL to navigate to when the user presses Back
func (h *Handler) RadioTune(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	tuneURL := r.URL.Query().Get("url")
	name := r.URL.Query().Get("name")
	backURL := r.URL.Query().Get("back")
	if backURL == "" {
		backURL = b + "/radio"
	}
	if name == "" {
		name = "Station"
	}
	if tuneURL == "" {
		h.writeText(w, r, "Radio", "No station selected.")
		return
	}

	resp, err := tuneinClient.Get(tuneURL)
	if err != nil {
		log.Printf("tunein tune %s: %v", tuneURL, err)
		h.writeText(w, r, name, "Stream unavailable:\n"+err.Error())
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		h.writeText(w, r, name, "Read error.")
		return
	}

	streams := extractStreamURLs(data)
	if len(streams) == 0 {
		h.writeText(w, r, name, "No stream URL found.")
		return
	}

	menu := PhoneMenu{
		Title: name,
		MenuItems: []MenuItem{
			{Name: "< Back", URL: actionURL(backURL)},
		},
		SoftKeys: oneLevelBackKeys(backURL),
	}

	// Build text with stream info; send as a text page with a back softkey.
	var sb strings.Builder
	for i, s := range streams {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(s)
	}
	_ = menu
	h.writeText(w, r, name, sb.String())
}

// extractStreamURLs parses the raw body of a TuneIn Tune.ashx response and
// returns the stream URL(s) it contains.  Handles OPML, PLS, M3U, and
// plain-text (one URL per line) formats.
func extractStreamURLs(data []byte) []string {
	var streams []string
	body := strings.TrimSpace(string(data))

	// OPML – TuneIn may return an opml doc for certain station types.
	if strings.Contains(body, "<opml") {
		var doc opmlDoc
		if xml.Unmarshal(data, &doc) == nil {
			for _, o := range doc.Body.Outlines {
				if o.Type == "audio" && strings.HasPrefix(o.URL, "http") {
					streams = append(streams, o.URL)
				}
			}
			if len(streams) > 0 {
				return streams
			}
		}
	}

	// PLS / M3U / plain-text – parse line by line.
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		// PLS: File1=http://...
		if lower := strings.ToLower(line); strings.HasPrefix(lower, "file") && strings.Contains(line, "=") {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				if u := strings.TrimSpace(parts[1]); strings.HasPrefix(u, "http") {
					streams = append(streams, u)
					continue
				}
			}
		}
		if strings.HasPrefix(line, "http") {
			streams = append(streams, line)
		}
	}
	return streams
}

// truncate shortens s to at most n runes, appending "…" if needed.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
