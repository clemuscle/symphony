// Package stacks embarque le catalogue officiel de Symphony : un dossier par
// stack (<nom>/stack.yaml, ses CI, son squelette). Elles sont proposées à
// côté de celles du repo du client quand la configuration les expose
// (stacks.official), sans accès réseau : elles suivent la version de Symphony.
package stacks

import (
	"embed"
	"io/fs"
)

//go:embed all:*
var files embed.FS

// FS : le catalogue officiel, un dossier par stack
func FS() fs.FS { return files }
