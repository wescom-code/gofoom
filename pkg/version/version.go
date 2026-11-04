package version

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// Name of the application
	Name = "Gofoom"

	// Version of the application (SemVer format: Major.Minor.Patch)
	Version = "1.2.1"

	// Description of the application
	Description = "High-Performance Proxy Facade with PAC, SOCKS5 & Fallback"

	// Copyright notice
	Copyright = "Copyright (c) 2026 Gofoom Authors"
)

// Info holds structured version metadata
type Info struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Copyright   string `json:"copyright"`
}

// GetInfo returns the application's version metadata
func GetInfo() Info {
	return Info{
		Name:        Name,
		Version:     Version,
		Description: Description,
		Copyright:   Copyright,
	}
}

// FullString returns formatted version string e.g. "Gofoom v1.2.0"
func FullString() string {
	return fmt.Sprintf("%s v%s", Name, Version)
}

// ParseSemVer splits Version into major, minor, patch integers
func ParseSemVer() (major, minor, patch int) {
	parts := strings.Split(Version, ".")
	if len(parts) >= 1 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}
