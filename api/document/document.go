// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// Package document is the host document dialect adaptation every host shares:
// a host document carries the providers, governance and application sections
// its own deployment needs over the core declaration, and the host maps that
// whole document onto a Document — the declaration-shaped section every host
// has in common. The dialect shifts from host to host, the declaration
// underneath stays fixed: Declaration derives the organization declaration from
// a Document — the entry agent validation, the assembly order and the agent
// sections — so a host never spells the declaration rules a second time.
package document

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
)

// ErrDeclarationEntry reports a document that names no entry agent, or names
// one it does not declare: the entry is the one agent the declaration admits
// application input to, so a document without it assembles nothing.
var ErrDeclarationEntry = errors.New("config: the document names no declared entry agent")

// Document is the declaration-shaped section of a host document: the entry
// agent, the agent sections keyed by identity and the MCP servers the
// organization binds. A host whose own document carries further sections maps
// those onto the document schema it decodes and hands this section over.
type Document struct {
	// Entry is the name of the entry agent that receives application input.
	Entry string
	// Agents holds the declared agent sections keyed by identity. The entry
	// is resolved against this map, so an identity the map does not carry is
	// outside the document.
	Agents map[string]config.AgentSpec
	// Servers declares the MCP servers the organization binds by name.
	Servers map[string]config.MCPServerSpec
}

// Order returns the declared agent identities in assembly order: the entry
// first, then the remaining identities sorted, so one document assembles in
// one order across restarts.
func (d Document) Order() []string {
	entry := strings.TrimSpace(d.Entry)
	ids := make([]string, 0, len(d.Agents))
	for id := range d.Agents {
		if id != entry {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return append([]string{entry}, ids...)
}

// Declaration derives the organization declaration of this document: the entry
// agent is required and must be declared, and the agent sections are assembled
// in Order. Every other section of a host document stays the host's own, so a
// document that carries a connection registry or an application section keeps
// it beside the declaration instead of inside it.
func (d Document) Declaration() (*config.Declaration, error) {
	entry := strings.TrimSpace(d.Entry)
	if entry == "" {
		return nil, ErrDeclarationEntry
	}
	if _, ok := d.Agents[entry]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrDeclarationEntry, entry)
	}
	decl := &config.Declaration{Entry: entry, Servers: d.Servers}
	for _, id := range d.Order() {
		spec := d.Agents[id]
		spec.ID = id
		decl.Agents = append(decl.Agents, spec)
	}
	return decl, nil
}

// PromptFiles resolves the prompt files one declared agent is assembled from,
// relative to the prompt directory the document names. A declared file the
// checkout does not carry is reported as absent instead of failing the read, so
// the personal context a deployment keeps beside the checkout stays an input
// rather than a startup precondition; any other read failure stops the read.
func PromptFiles(dir string, names []string) ([]config.FileRef, []string, error) {
	files := make([]config.FileRef, 0, len(names))
	var absent []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				absent = append(absent, path)
				continue
			}
			return nil, nil, fmt.Errorf("config: read the declared prompt file %s: %w", path, err)
		}
		files = append(files, config.FileRef{Path: path, Kind: config.FileKindPrompt})
	}
	return files, absent, nil
}
