package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"gofoom/pkg/version"
)

type VersionQuad struct {
	Major int `json:"Major"`
	Minor int `json:"Minor"`
	Patch int `json:"Patch"`
	Build int `json:"Build"`
}

type FixedFileInfo struct {
	FileVersion    VersionQuad `json:"FileVersion"`
	ProductVersion VersionQuad `json:"ProductVersion"`
	FileFlagsMask  string      `json:"FileFlagsMask"`
	FileFlags      string      `json:"FileFlags "`
	FileOS         string      `json:"FileOS"`
	FileType       string      `json:"FileType"`
	FileSubType    string      `json:"FileSubType"`
}

type StringFileInfo struct {
	Comments         string `json:"Comments"`
	CompanyName      string `json:"CompanyName"`
	FileDescription  string `json:"FileDescription"`
	FileVersion      string `json:"FileVersion"`
	InternalName     string `json:"InternalName"`
	LegalCopyright   string `json:"LegalCopyright"`
	LegalTrademarks  string `json:"LegalTrademarks"`
	OriginalFilename string `json:"OriginalFilename"`
	PrivateBuild     string `json:"PrivateBuild"`
	ProductName      string `json:"ProductName"`
	ProductVersion   string `json:"ProductVersion"`
	SpecialBuild     string `json:"SpecialBuild"`
}

type VarFileInfo struct {
	Translation struct {
		LangID    string `json:"LangID"`
		CharsetID string `json:"CharsetID"`
	} `json:"Translation"`
}

type VersionConfig struct {
	FixedFileInfo  FixedFileInfo  `json:"FixedFileInfo"`
	StringFileInfo StringFileInfo `json:"StringFileInfo"`
	VarFileInfo    VarFileInfo    `json:"VarFileInfo"`
	IconPath       string         `json:"IconPath,omitempty"`
}

func main() {
	major, minor, patch := version.ParseSemVer()
	fileVerStr := fmt.Sprintf("%d.%d.%d.0", major, minor, patch)

	quad := VersionQuad{
		Major: major,
		Minor: minor,
		Patch: patch,
		Build: 0,
	}

	cfg := VersionConfig{
		FixedFileInfo: FixedFileInfo{
			FileVersion:    quad,
			ProductVersion: quad,
			FileFlagsMask:  "3f",
			FileFlags:      "00",
			FileOS:         "040004",
			FileType:       "01",
			FileSubType:    "00",
		},
		StringFileInfo: StringFileInfo{
			Comments:         version.Description,
			CompanyName:      "Gofoom Authors",
			FileDescription:  "Gofoom Proxy Server",
			FileVersion:      fileVerStr,
			InternalName:     "gofoom",
			LegalCopyright:   version.Copyright,
			LegalTrademarks:  "",
			OriginalFilename: "gofoom.exe",
			PrivateBuild:     "",
			ProductName:      version.Name,
			ProductVersion:   version.Version,
			SpecialBuild:     "",
		},
		IconPath: "../../build/app.ico",
	}
	cfg.VarFileInfo.Translation.LangID = "0409"
	cfg.VarFileInfo.Translation.CharsetID = "04B0"

	// Find project root
	root := "."
	if _, err := os.Stat("pkg/version/version.go"); err != nil {
		if _, err := os.Stat("../pkg/version/version.go"); err == nil {
			root = ".."
		} else if _, err := os.Stat("../../pkg/version/version.go"); err == nil {
			root = "../.."
		}
	}

	jsonPath := filepath.Join(root, "cmd", "gofoom", "versioninfo.json")
	sysoPath := filepath.Join(root, "cmd", "gofoom", "resource.syso")

	data, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		log.Fatalf("Failed to marshal versioninfo: %v", err)
	}

	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", jsonPath, err)
	}
	fmt.Printf("[genversion] Wrote %s (v%s)\n", jsonPath, version.Version)

	// Locate goversioninfo executable
	toolPath, err := exec.LookPath("goversioninfo")
	if err != nil {
		candidates := []string{
			filepath.Join(os.Getenv("GOPATH"), "bin", "goversioninfo.exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "go", "bin", "goversioninfo.exe"),
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				toolPath = cand
				break
			}
		}
	}

	if toolPath == "" {
		fmt.Println("[genversion] Installing goversioninfo...")
		installCmd := exec.Command("go", "install", "github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			log.Fatalf("[genversion] Failed to install goversioninfo: %v", err)
		}
		toolPath = filepath.Join(os.Getenv("USERPROFILE"), "go", "bin", "goversioninfo.exe")
	}

	// Run goversioninfo in cmd/gofoom directory
	cmd := exec.Command(toolPath, "-o", "resource.syso", "versioninfo.json")
	cmd.Dir = filepath.Join(root, "cmd", "gofoom")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("[genversion] Failed to run goversioninfo: %v", err)
	}

	fmt.Printf("[genversion] Generated %s successfully.\n", sysoPath)
}
