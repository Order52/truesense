// Package models knows which laptops TrueSense supports, and holds a probe that learns about
// the rest WITHOUT changing anything.
//
// Three tiers, because firmware control must never be guessed:
//
//	supported    verified on real hardware (a maintainer or a user report): everything works and is tested
//	unverified   an Acer Predator/Nitro with no verified report. The kernel module refuses to bind (it can
//	             be forced, at your own risk). `truesense probe` only READS, and its output is what a
//	             maintainer needs to move the model to "supported" in a release
//	unsupported  not an Acer: TrueSense has nothing to do here
package models

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DMIRoot is where the kernel publishes the machine's identity.
const DMIRoot = "/sys/class/dmi/id"

// ReportURL is where a user of an unverified model sends the probe report.
const ReportURL = "https://github.com/Order52/truesense/issues/new?template=new_model_report.yml"

// GamingWMIGUID is the Acer gaming WMI interface TrueSense talks to; its presence is the first
// sign that a model has it.
const GamingWMIGUID = "7a4ddfe7-5b5d-40b4-8595-4408e0cc7f56"

// Tier says how far TrueSense trusts a model.
type Tier string

// The three tiers.
const (
	TierSupported   Tier = "supported"
	TierUnverified  Tier = "unverified"
	TierUnsupported Tier = "unsupported"
)

// Known is one verified model.
type Known struct {
	Family       string
	VerifiedBIOS []string
	Report       string
}

// Supported lists the verified models by DMI product name. Add a model here only with a
// verified hardware report (docs/STATUS.md): the kernel module matches the same names.
var Supported = map[string]Known{
	"Predator PH16-71": {Family: "Predator Helios 16", VerifiedBIOS: []string{"V1.18"}, Report: "docs/STATUS.md"},
}

var predator = regexp.MustCompile(`(?i)^(Predator|Nitro)\s+[A-Z]{1,4}\d{2}-\d{2}`)

// Model is what Identify learns from the DMI tables. The JSON names are the 0.3.x probe's.
type Model struct {
	Vendor       string `json:"vendor"`
	Product      string `json:"product"`
	BIOS         string `json:"bios"`
	BIOSDate     string `json:"bios_date"`
	Family       string `json:"family"`
	BIOSVerified *bool  `json:"bios_verified"` // nil unless the model is supported
	Tier         Tier   `json:"tier"`
	Message      string `json:"message"`
}

func readDMI(root, name string) string {
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func isAcer(vendor string) bool { return strings.HasPrefix(strings.ToLower(vendor), "acer") }

// Identify reads the machine's DMI identity under root (DMIRoot on a real system) and grades
// it. A missing directory is an unsupported machine, never an error.
func Identify(root string) Model {
	vendor, product := readDMI(root, "sys_vendor"), readDMI(root, "product_name")
	bios, date := readDMI(root, "bios_version"), readDMI(root, "bios_date")
	m := Model{Vendor: vendor, Product: product, BIOS: bios, BIOSDate: date}
	if m.Product == "" {
		m.Product = "unknown"
	}
	if m.BIOS == "" {
		m.BIOS = "?"
	}
	known, isKnown := Supported[product]
	switch {
	case isKnown && isAcer(vendor): // the kernel module matches vendor AND name: so does this
		verified := slices.Contains(known.VerifiedBIOS, bios)
		m.Tier, m.Family, m.BIOSVerified = TierSupported, known.Family, &verified
		m.Message = fmt.Sprintf("%s, BIOS %s", product, m.BIOS)
		if !verified {
			m.Message += fmt.Sprintf(" (verified on BIOS %s: yours is newer or older, which is expected to work: tell us if it does not)", strings.Join(known.VerifiedBIOS, ", "))
		}
	case isAcer(vendor) && predator.MatchString(product):
		m.Tier, m.Family = TierUnverified, "similar Acer gaming laptop"
		m.Message = fmt.Sprintf("%s is a similar Acer laptop that TrueSense has not been verified on. Nothing is written to its firmware. `truesense probe` reads what it answers (read-only); "+
			"share the report so it can be supported: %s", product, ReportURL)
	default:
		shownProduct, shownVendor := product, vendor
		if shownProduct == "" {
			shownProduct = "unknown"
		}
		if shownVendor == "" {
			shownVendor = "unknown vendor"
		}
		m.Tier = TierUnsupported
		m.Message = fmt.Sprintf("'%s' (%s) is not an Acer Predator: TrueSense does not apply", shownProduct, shownVendor)
	}
	return m
}
