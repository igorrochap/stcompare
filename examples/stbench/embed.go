// Package stbenchadapters exposes the canonical adapter examples embedded in stbench.
package stbenchadapters

import (
	"embed"
	"slices"
)

// Role identifies the generated stbench adapter config key.
type Role string

const (
	RoleCloud       Role = "cloud"
	RoleLocal       Role = "local"
	RoleCodingAgent Role = "coding-agent"
)

// AdapterFile describes one canonical file installed by stbench init.
// Role is empty for support files that do not receive their own command.
type AdapterFile struct {
	Role     Role
	Filename string
}

// files contains only the adapters installed by stbench init.
//
//go:embed adapter.py local_model_adapter.py coding_agent_adapter.py _protocol.py
var files embed.FS

var adapterFiles = []AdapterFile{
	{Role: RoleCloud, Filename: "adapter.py"},
	{Role: RoleLocal, Filename: "local_model_adapter.py"},
	{Role: RoleCodingAgent, Filename: "coding_agent_adapter.py"},
	{Filename: "_protocol.py"},
}

// Files returns descriptors for the embedded canonical adapter files.
func Files() []AdapterFile {
	return slices.Clone(adapterFiles)
}

// Contents returns the embedded contents of the described file.
func (f AdapterFile) Contents() ([]byte, error) {
	return files.ReadFile(f.Filename)
}
