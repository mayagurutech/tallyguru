// Package gst holds GST rules used by the checks: GSTIN format and check digit, and state
// codes. The tool only reports what it finds; whether a return or credit is affected is for
// the user to judge.
package gst

import (
	"regexp"
	"strings"
)

// States maps GST state codes to state names (as used on GSTINs).
var States = map[string]string{
	"01": "Jammu and Kashmir", "02": "Himachal Pradesh", "03": "Punjab", "04": "Chandigarh", "05": "Uttarakhand",
	"06": "Haryana", "07": "Delhi", "08": "Rajasthan", "09": "Uttar Pradesh", "10": "Bihar", "11": "Sikkim",
	"12": "Arunachal Pradesh", "13": "Nagaland", "14": "Manipur", "15": "Mizoram", "16": "Tripura", "17": "Meghalaya",
	"18": "Assam", "19": "West Bengal", "20": "Jharkhand", "21": "Odisha", "22": "Chhattisgarh", "23": "Madhya Pradesh",
	"24": "Gujarat", "25": "Daman and Diu", "26": "Dadra and Nagar Haveli and Daman and Diu", "27": "Maharashtra",
	"28": "Andhra Pradesh (old)", "29": "Karnataka", "30": "Goa", "31": "Lakshadweep", "32": "Kerala", "33": "Tamil Nadu",
	"34": "Puducherry", "35": "Andaman and Nicobar Islands", "36": "Telangana", "37": "Andhra Pradesh", "38": "Ladakh",
	"97": "Other Territory", "99": "Centre Jurisdiction",
}

// norm makes state names comparable: lower case, "&" as "and", no punctuation or spaces.
func norm(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "&", "and"))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SameState says whether two state names are the same state, allowing for spelling
// variants seen in Tally ("Jammu & Kashmir", "Orissa").
func SameState(a, b string) bool {
	alias := map[string]string{"orissa": "odisha", "pondicherry": "puducherry", "uttaranchal": "uttarakhand",
		"andhrapradeshold": "andhrapradesh", "nctofdelhi": "delhi", "newdelhi": "delhi"}
	x, y := norm(a), norm(b)
	if v, ok := alias[x]; ok {
		x = v
	}
	if v, ok := alias[y]; ok {
		y = v
	}
	return x != "" && x == y
}

// StateOf gives the state name for a GSTIN's first two digits ("" if unknown).
func StateOf(gstin string) string {
	if len(gstin) < 2 {
		return ""
	}
	return States[gstin[:2]]
}

var reGSTIN = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

// Also accepted: UIN/NRTP/TDS/TCS formats differ; those are reported as "not a regular GSTIN
// format" for the user to look at, never as wrong.

const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// checkDigit computes the 15th character from the first 14 (GSTN's published method).
func checkDigit(s string) byte {
	sum := 0
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(chars, s[i])
		f := 1
		if i%2 == 1 {
			f = 2
		}
		p := v * f
		sum += p/36 + p%36
	}
	return chars[(36-sum%36)%36]
}

// Problem is what is wrong with a GSTIN, or "" when it looks right.
func Problem(gstin string) string {
	g := strings.ToUpper(strings.TrimSpace(gstin))
	switch {
	case g == "":
		return ""
	case len(g) != 15:
		return "not 15 characters"
	case !reGSTIN.MatchString(g):
		return "not in the regular GSTIN format"
	case States[g[:2]] == "":
		return "unknown state code " + g[:2]
	case checkDigit(g) != g[14]:
		return "check digit does not match (typing mistake likely)"
	}
	return ""
}
