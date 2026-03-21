// Package minibrowser provides the XML types used by the Snom IP phone
// minibrowser and the HTTP handlers that serve them.
//
// Snom XML Minibrowser reference:
// https://github.com/Snomio/Documentation/blob/master/docs/xml_minibrowser/index.md
package minibrowser

import "encoding/xml"

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// ---- SnomIPPhoneMenu -------------------------------------------------------

// PhoneMenu renders as <SnomIPPhoneMenu>.
type PhoneMenu struct {
	XMLName     xml.Name     `xml:"SnomIPPhoneMenu"`
	Title       string       `xml:"Title,omitempty"`
	MenuItems   []MenuItem   `xml:"MenuItem,omitempty"`
	SoftKeys    []SoftKey    `xml:"SoftKeyItem,omitempty"`
}

// URLRef renders as <URL> with optional attributes like track="no".
type URLRef struct {
	Track string `xml:"track,attr,omitempty"`
	Value string `xml:",chardata"`
}

// MenuItem is a single selectable line inside a PhoneMenu.
// The URL attribute is the HTTP endpoint the phone fetches when the user
// selects this item.
type MenuItem struct {
	Name string `xml:"name,attr"`
	URL  URLRef `xml:"URL"`
}

// SoftKey maps a function key label to a URL action.
type SoftKey struct {
	Name string `xml:"name,attr"`
	URL  URLRef `xml:"URL"`
}

// ---- SnomIPPhoneText -------------------------------------------------------

// PhoneText renders as <SnomIPPhoneText>.
type PhoneText struct {
	XMLName xml.Name `xml:"SnomIPPhoneText"`
	Title   string   `xml:"Title,omitempty"`
	Text    string   `xml:"Text"`
}

// ---- SnomIPPhoneInput ------------------------------------------------------

// PhoneInput renders as <SnomIPPhoneInput> for single-line text entry.
type PhoneInput struct {
	XMLName   xml.Name  `xml:"SnomIPPhoneInput"`
	Track     string    `xml:"track,attr,omitempty"`
	Title     string    `xml:"Title,omitempty"`
	URL       InputURL  `xml:"URL"`
	InputItem InputItem `xml:"InputItem"`
	SoftKeys  []SoftKey `xml:"SoftKeyItem,omitempty"`
}

// InputURL is the endpoint that receives the user's input via query parameter.
type InputURL struct {
	Value string `xml:",chardata"`
}

// InputItem configures the actual editable text field for SnomIPPhoneInput.
type InputItem struct {
	DisplayName  string `xml:"DisplayName,omitempty"`
	DefaultValue string `xml:"DefaultValue,omitempty"`
	InputToken   string `xml:"InputToken,omitempty"`
	InputMask    string `xml:"InputMask,omitempty"`
	InputFlags   string `xml:"InputFlags,omitempty"`
}
