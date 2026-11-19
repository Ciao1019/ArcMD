// Package bundle embeds the compiled official Cloud Sync WASM plugin archive.
package bundle

import (
	_ "embed"

	"github.com/Ciao1019/ArcMD/internal/plugin"
)

//go:embed cloud-sync.zip
var CloudSyncZip []byte

// CatalogPlugin describes an official bundled plugin ready to inspect and install.
type CatalogPlugin struct {
	Manifest *plugin.Manifest
	Listing  plugin.Listing
	Archive  []byte
}

// OfficialCatalog returns the bundled official plugins with verified manifest and SHA-256.
func OfficialCatalog() []CatalogPlugin {
	man, shaHex, err := plugin.InspectArchive(CloudSyncZip)
	if err != nil {
		panic(err)
	}
	return []CatalogPlugin{
		{
			Manifest: man,
			Listing: plugin.Listing{
				ID:          man.ID,
				Name:        man.Name,
				Version:     man.Version,
				Description: man.Description,
				Author:      man.Author,
				Homepage:    man.Homepage,
				Icon:        man.Icon,
				Tags:        []string{"sync", "s3", "webdav"},
				Permissions: man.Permissions,
				SHA256:      shaHex,
			},
			Archive: CloudSyncZip,
		},
	}
}
